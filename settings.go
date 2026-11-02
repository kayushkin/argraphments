package main

import (
	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/llm-bridge/servicesettings"
)

// ServiceName is this service's name in its own settings description.
const ServiceName = "argraphments"

// OwnedEnvironmentVariablePrefix is the prefix of the variables that are this
// service's alone. A set variable carrying it that SettingDefinitions does not
// declare stops the service from starting.
const OwnedEnvironmentVariablePrefix = "ARGRAPHMENTS_"

// Keys of the settings.
const (
	SettingListenAddress         = "listen_address"
	SettingDatabasePath          = "database_path"
	SettingAuthStoreURL          = "auth_store_url"
	SettingAuthStoreToken        = "auth_store_token"
	SettingAnthropicCredentialID = "anthropic_credential_id"
	SettingOpenAICredentialID    = "openai_credential_id"
)

// SettingDefinitions declares every ARGRAPHMENTS_ variable the server reads, once.
func SettingDefinitions() []servicesettings.Definition {
	return []servicesettings.Definition{
		{Key: SettingListenAddress, EnvironmentVariable: "ARGRAPHMENTS_ADDR", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Default: "127.0.0.1:8086",
			Description: "The address the server listens on. Keep it on loopback; nginx serves it under /argraphments."},
		{Key: SettingDatabasePath, EnvironmentVariable: "ARGRAPHMENTS_DB", Kind: msg.ServiceSettingKindPath, ValueType: msg.ServiceSettingValueTypeString, Default: "./argraphments.db",
			Description: "The SQLite file transcripts, claims and speakers are kept in."},
		{Key: SettingAuthStoreURL, EnvironmentVariable: "ARGRAPHMENTS_AUTH_STORE_URL", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "auth-store's base URL. Both model keys are read from there at start, never from a file."},
		{Key: SettingAuthStoreToken, EnvironmentVariable: "ARGRAPHMENTS_AUTH_STORE_TOKEN", Kind: msg.ServiceSettingKindSecret, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "auth-store's bearer token. Host-local drop-in only, mode 600."},
		{Key: SettingAnthropicCredentialID, EnvironmentVariable: "ARGRAPHMENTS_ANTHROPIC_CREDENTIAL_ID", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The auth-store credential (provider anthropic, field api_key) used to pull an argument tree out of a transcript."},
		{Key: SettingOpenAICredentialID, EnvironmentVariable: "ARGRAPHMENTS_OPENAI_CREDENTIAL_ID", Kind: msg.ServiceSettingKindWiring, ValueType: msg.ServiceSettingValueTypeString, Required: true,
			Description: "The auth-store credential (provider openai, field api_key) used to transcribe recordings with Whisper."},
	}
}

// NewSettingsRegistry reads this service's settings from environment and
// refuses to go on while a required one is unset.
func NewSettingsRegistry(environment servicesettings.Environment) (*servicesettings.Registry, error) {
	registry, err := servicesettings.New(ServiceName, []string{OwnedEnvironmentVariablePrefix}, SettingDefinitions(), environment)
	if err != nil {
		return nil, err
	}
	if err := registry.CheckRequired(); err != nil {
		return nil, err
	}
	return registry, nil
}
