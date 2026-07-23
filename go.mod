module github.com/confiify/confii-go

go 1.25.0

toolchain go1.25.8

// Direct dependencies for the core module. Cloud integrations and their SDKs
// live in the independently tidy loader/cloud and secret/cloud modules; see
// docs/installation.md for opt-in instructions.
require (
	github.com/BurntSushi/toml v1.6.0
	github.com/fsnotify/fsnotify v1.9.0
	github.com/go-playground/validator/v10 v10.30.1
	github.com/mitchellh/mapstructure v1.5.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	gopkg.in/ini.v1 v1.67.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gabriel-vasile/mimetype v1.4.12 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
