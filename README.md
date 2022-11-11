# schema-deletion-tool
Tool for discovering and deleting unused schemas from Schema Registry. This tool is
recommended to be used as a plugin for [Confluent CLI](https://docs.confluent.io/confluent-cli/current/overview.html).
Only Confluent Cloud is supported for now.

The schema deletion tool (plugin) relies on Confluent CLI for functionalities such as 
logging in, setting up environments and so on. Refer to [Usage](#Usage) for an example. Also refer
to [Confluent CLI Plugin](https://docs.confluent.io/confluent-cli/current/command-reference/plugin/index.html) for more information.

## Contribution
All contributions are welcomed! Follow the below guidance to set up development environment, build and test
before sending out the PR.

### Development Environment

Start by following these steps to set up your computer for CLI development:

#### Go Version

We recommend you use [goenv](https://github.com/syndbg/goenv) to manage your Go versions.
There's a `.go-version` file in this repo with the exact version we use.

We recommend cloning the `goenv` repo directly to ensure that you have access to the latest version of Go. If you've
already installed `goenv` with brew, uninstall it first:

    brew uninstall goenv

Now, clone the `goenv` repo:

    git clone https://github.com/syndbg/goenv.git ~/.goenv

Then, add the following to your shell profile:

    export GOENV_ROOT="$HOME/.goenv"
    export PATH="$GOENV_ROOT/bin:$PATH"
    eval "$(goenv init -)"
    export PATH="$PATH:$GOPATH/bin"

Finally, you can install the appropriate version of Go by running the following command inside the root directory of the repository:

    goenv install

#### Build

This CLI tool can either be invoked as a standalone script, or as a CLI plugin.
Both will generate an binary executable that you may want to put in $PATH for easier access.

To build as a standalone executable:

    make build-local

To build as a CLI plugin:

    make build-plugin

#### Run tests

    make test

## Usage

### Prerequisites

    # Login to Confluent Cloud if you haven't. Otherwise you'd get an error.
    confluent login

    # Select the environment you're interested in. The tool assumes topics using the 
    # the schemas registered in Schema Registry in the same environment.
    # run 'confluent environment list' to see all available environments.
    confluent environment use <desired env>

    # Make sure you have proper access to Schema Registry (API key etc.) and see
    # all available subjects for cleanup.
    confluent schema-registry subject list

### Command

Below we show how the executable can be used as a CLI plugin that can be invoked by
Confluent CLI. Using this as CLI plugin gives you a unified experience when using Confluent CLI.

**Make sure the executable `confluent-schema_registry-cleanup` is put under $PATH for CLI to access. Refer to [Confluent CLI Plugin](https://docs.confluent.io/confluent-cli/current/command-reference/plugin/index.html) for how this works.**

If you prefer to use the tool as a standalone script, simply invoke the tool with name of the executable,
which is `schema-deletion-tool` by default.

    # Cleanup unused schemas from specified subject.
    confluent schema-registry cleanup --subject mytopic-value

    # Cleanup all eligible subjects with TopicNameStrategy
    confluent schema-registry cleanup --all

    # Provide Kafka cluster API keys when cleaning up (otherwise will be prompted)
    confluent schema-registry cleanup --subject mytopic-value --config-file /path/to/config

The config file, if provided, should look like:

    {
		"lkc-123": {
			"key": "api-key-for-lkc-123",
			"secret": "api-secret-for-lkc-123"
		},
		"lkc-456": {
			"key": "api-key-for-lkc-456",
			"secret": "api-secret-for-lkc-456"
		}
    }

### Steps

Once running, the CLI tool will run the following steps leading to cleaning 
up unused schemas.

<ol>
    <li>(optional) Find out all subjects with TopicNameStrategy naming conventions, i.e. subjects
    that end with "-key" or "-value" suffix.</li>
    <li>List all Kafka clusters in the environment. You will have a chance to specify the list of
    clusters you want to skip scanning. You will be prompted for API keys if you didn't specify config-file
    when running the command.</li>
    <li>For each eligible subjects, find out the corresponding topic names based on TopicNameStrategy.
    Note: There can be multiple topics in different clusters with the same name.</li>
    <li>Consume from the topics to identify the schema IDs that are in use, compare with the complete
    set of schema IDs obtained from Schema Registry and give list of deletion candidates.</li>
    <li>Confirm and soft/hard delete the schemas not in use.</li>
</ol>
