package repositories

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

const (
	configurationEnvironmentVar = "LBM_CONFIG_ENV"
	environmentConfigRoot       = "configuration/environments"
	configurationEnvPrefix      = "LBM_CFG_"
)

//go:embed configuration/default.yaml
var embeddedDefaultConfiguration []byte

//go:embed configuration/environments/*.yaml
var embeddedEnvironmentConfigurations embed.FS

// AppConfiguration is the root application configuration object.
type AppConfiguration struct {
	Grpc    GrpcConfiguration    `yaml:"grpc"`
	Haproxy HaproxyConfiguration `yaml:"haproxy"`
}

// GrpcConfiguration stores gRPC server settings.
type GrpcConfiguration struct {
	Address string `yaml:"address"`
}

// HaproxyConfiguration stores HAProxy runtime and file settings.
type HaproxyConfiguration struct {
	ConfigurationFile string                     `yaml:"configuration_file"`
	Socket            HaproxySocketConfiguration `yaml:"socket"`
}

// HaproxySocketConfiguration stores HAProxy admin socket connection settings.
type HaproxySocketConfiguration struct {
	Network string        `yaml:"network"`
	Address string        `yaml:"address"`
	Timeout time.Duration `yaml:"timeout"`
}

// AppConfigurationService loads and caches effective application configuration.
type AppConfigurationService struct {
	logger              zerolog.Logger
	cli                 *CLIRepository
	cachedConfiguration *AppConfiguration
}

func newConfigurationService(logger zerolog.Logger, cli *CLIRepository) AppConfigurationService {
	return AppConfigurationService{
		logger:              logger.With().Str("component", "configuration_service").Logger(),
		cli:                 cli,
		cachedConfiguration: nil,
	}
}

// GetConfiguration returns the merged configuration from embedded defaults, env layer and overrides.
func (a *AppConfigurationService) GetConfiguration() (*AppConfiguration, error) {
	if a.cachedConfiguration == nil {
		var configuration AppConfiguration

		if err := applyYamlLayer("embedded default configuration", embeddedDefaultConfiguration, &configuration); err != nil {
			return nil, err
		}

		environment := strings.TrimSpace(os.Getenv(configurationEnvironmentVar))
		if environment != "" {
			envConfigurationFilePath := filepath.ToSlash(filepath.Join(environmentConfigRoot, environment+".yaml"))

			envConfigurationFileContent, err := embeddedEnvironmentConfigurations.ReadFile(envConfigurationFilePath)
			if err != nil {
				return nil, fmt.Errorf("unable to load environment configuration %q from %s: %w", environment, envConfigurationFilePath, err)
			}

			if err := applyYamlLayer("embedded environment configuration", envConfigurationFileContent, &configuration); err != nil {
				return nil, err
			}
		}

		configurationFilePath := a.cli.GetConfFilePath()
		if strings.TrimSpace(configurationFilePath) != "" {
			configurationFileContent, err := os.ReadFile(configurationFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					a.logger.Debug().Str("path", configurationFilePath).Msg("External configuration file not found; skipping this override layer")
				} else {
					return nil, fmt.Errorf("unable to read configuration file %q: %w", configurationFilePath, err)
				}
			} else {
				if err := applyYamlLayer("external configuration", configurationFileContent, &configuration); err != nil {
					return nil, err
				}
			}
		}

		if err := applyEnvironmentOverrides(&configuration); err != nil {
			return nil, err
		}

		a.cachedConfiguration = &configuration
	}

	return a.cachedConfiguration, nil
}

func applyYamlLayer(layer string, content []byte, configuration *AppConfiguration) error {
	if err := yaml.Unmarshal(content, configuration); err != nil {
		return fmt.Errorf("unable to unmarshal %s: %w", layer, err)
	}

	return nil
}

func applyEnvironmentOverrides(configuration *AppConfiguration) error {
	for _, env := range os.Environ() {
		key, value, found := strings.Cut(env, "=")
		if !found || !strings.HasPrefix(key, configurationEnvPrefix) {
			continue
		}

		path := envKeyToPath(strings.TrimPrefix(key, configurationEnvPrefix))
		if len(path) == 0 {
			continue
		}

		if err := setNestedConfigurationValue(configuration, path, value); err != nil {
			return fmt.Errorf("unable to apply environment override from %s: %w", key, err)
		}
	}

	return nil
}

func envKeyToPath(key string) []string {
	if strings.TrimSpace(key) == "" {
		return nil
	}

	parts := strings.Split(key, "__")
	path := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			path = append(path, part)
		}
	}

	return path
}

func setNestedConfigurationValue(configuration any, path []string, rawValue string) error {
	current := reflect.ValueOf(configuration)
	if current.Kind() != reflect.Pointer || current.IsNil() {
		return fmt.Errorf("configuration target must be a non-nil pointer")
	}

	current = current.Elem()
	for i, pathPart := range path {
		if current.Kind() != reflect.Struct {
			return fmt.Errorf("%q does not target a struct node", strings.Join(path[:i], "."))
		}

		field, ok := findStructFieldByPathPart(current, pathPart)
		if !ok {
			return fmt.Errorf("unknown configuration key %q", strings.Join(path[:i+1], "."))
		}

		if i == len(path)-1 {
			if !field.CanSet() {
				return fmt.Errorf("field %q cannot be set", strings.Join(path, "."))
			}

			if err := assignValueFromString(field, rawValue); err != nil {
				return fmt.Errorf("invalid value for %q: %w", strings.Join(path, "."), err)
			}

			return nil
		}

		fieldValue := field
		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
			}
			fieldValue = fieldValue.Elem()
		}

		current = fieldValue
	}

	return nil
}

func findStructFieldByPathPart(structValue reflect.Value, pathPart string) (reflect.Value, bool) {
	structType := structValue.Type()
	normalizedPathPart := normalizeKey(pathPart)

	for i := range structType.NumField() {
		fieldType := structType.Field(i)
		if !fieldType.IsExported() {
			continue
		}

		yamlTag := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]
		if yamlTag == "-" {
			continue
		}

		if normalizeKey(yamlTag) == normalizedPathPart || normalizeKey(fieldType.Name) == normalizedPathPart {
			return structValue.Field(i), true
		}
	}

	return reflect.Value{}, false
}

func assignValueFromString(destination reflect.Value, rawValue string) error {
	target := destination
	if target.Kind() == reflect.Pointer {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		target = target.Elem()
	}

	if target.Type() == reflect.TypeOf(time.Duration(0)) {
		parsedDuration, err := time.ParseDuration(strings.TrimSpace(rawValue))
		if err != nil {
			return err
		}
		target.SetInt(int64(parsedDuration))
		return nil
	}

	decoded := reflect.New(target.Type())
	if err := yaml.Unmarshal([]byte(rawValue), decoded.Interface()); err != nil {
		return err
	}

	target.Set(decoded.Elem())
	return nil
}

func normalizeKey(input string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(input), "_", ""), "-", ""))
}
