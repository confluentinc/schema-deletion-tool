# schema-deletion-tool

Tool for discovering and deleting unused schemas from Schema Registry. Supports both
**Confluent Cloud** and **Confluent Platform (CP)**. Can be used as a standalone binary
or as a plugin for [Confluent CLI](https://docs.confluent.io/confluent-cli/current/overview.html).

_Note: Version 4 of the Confluent CLI is required. You can check the version you have installed by running `confluent version`_

## Features

- **Multi-platform**: Confluent Cloud (via CLI) and Confluent Platform (via REST API)
- **All naming strategies**: TopicNameStrategy, RecordNameStrategy, TopicRecordNameStrategy
- **Safety checks**: Schema references, migration rule chains, encryption rules, domain rules, global/contract rules, compatibility analysis
- **Dry-run & manifest workflow**: Scan once, review, delete later
- **Decoupled deletion phases**: Soft-delete and hard-delete independently
- **Schema context support**: Scope operations to specific contexts
- **Multi-cluster scanning**: Scan topics across multiple Kafka clusters

## Development

### Go Version

We recommend [goenv](https://github.com/syndbg/goenv) for Go version management.
The `.go-version` file pins the required version.

    goenv install

### Build

    # Standalone binary
    make build-local

    # CLI plugin
    make build-plugin

### Test

    # Unit tests
    make test

    # Integration tests (requires Docker)
    docker-compose up -d
    go test -v -tags=integration ./...
    docker-compose down

## Usage

### Confluent Cloud

#### Prerequisites

    confluent login
    confluent environment use <desired env>
    confluent schema-registry subject list

#### Commands

    # Clean specific subject
    confluent schema-registry cleanup --subject mytopic-value

    # Clean all eligible subjects
    confluent schema-registry cleanup --all

    # With credentials config file
    confluent schema-registry cleanup --all --config-file /path/to/config.json

Cloud credentials config file format:

```json
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
```

### Confluent Platform

    # Clean all subjects on CP
    ./schema-deletion-tool --platform cp --cp-config-file /path/to/cp-config.json --all

    # Clean specific subject
    ./schema-deletion-tool --platform cp --cp-config-file /path/to/cp-config.json --subject orders-value

CP config file format:

```json
{
    "schema_registry": {
        "url": "https://sr.example.com:8081",
        "auth": "basic",
        "username": "admin",
        "password": "secret"
    },
    "clusters": [
        {
            "name": "production-east",
            "bootstrap_servers": "broker1:9092,broker2:9092",
            "security_protocol": "SASL_SSL",
            "sasl_mechanism": "PLAIN",
            "sasl_username": "user1",
            "sasl_password": "pass1"
        },
        {
            "name": "production-west",
            "bootstrap_servers": "broker3:9092,broker4:9092",
            "security_protocol": "PLAINTEXT"
        }
    ]
}
```

Supported SR auth types: `basic`, `bearer`, `mtls`, `none`

Supported Kafka security protocols: `PLAINTEXT`, `SASL_PLAINTEXT`, `SASL_SSL`, `SSL`

Supported SASL mechanisms: `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512`

For mTLS, add SSL paths to the SR config and/or cluster config:
```json
{
    "ssl_ca_location": "/path/to/ca.pem",
    "ssl_cert_location": "/path/to/client.pem",
    "ssl_key_location": "/path/to/client.key"
}
```

### Naming Strategies

    # TopicNameStrategy (default) - subjects end with -key or -value
    ./schema-deletion-tool --all --strategy topic-name

    # RecordNameStrategy - requires explicit topic list or --scan-all-topics
    ./schema-deletion-tool --all --strategy record-name --topics orders,payments

    # TopicRecordNameStrategy - auto-resolves topics via prefix matching
    ./schema-deletion-tool --all --strategy topic-record-name

    # Scan all topics (works with any strategy)
    ./schema-deletion-tool --all --strategy record-name --scan-all-topics

### Schema Contexts

Schema contexts allow you to organize schemas into isolated namespaces using the
`:.context:` prefix (e.g., `:.staging:orders-value`, `:.production:orders-value`).

    # Process ALL contexts (default — no --context flag)
    # Includes default context (no prefix) and all named contexts
    ./schema-deletion-tool --all --dry-run

    # Scope to a specific named context only
    ./schema-deletion-tool --all --context staging

    # Scope to the default context only (subjects without any context prefix)
    ./schema-deletion-tool --all --context ""

    # Works with any platform and strategy
    ./schema-deletion-tool --platform cp --cp-config-file config.json --all --context production

    # Clean up an entire context
    ./schema-deletion-tool --platform cp --cp-config-file config.json --all --context staging \
      --soft-delete --force

When `--context` is omitted, schemas from **all contexts** are included. When specified,
only subjects matching that exact context are processed. Use `--context ""` to target
only the default (uncontexted) subjects.

### Dry-Run & Manifest Workflow

The recommended workflow for production environments:

    # Step 1: Discover unused schemas and save manifest
    ./schema-deletion-tool --all --dry-run --output candidates.json

    # Step 2: Review the manifest file
    # - Remove entries you want to keep
    # - Review blocked/warned schemas
    # - Override statuses if needed (e.g., "blocked_by_references" -> "safe")

    # Step 3: Soft-delete (reversible)
    ./schema-deletion-tool --from-file candidates.json --soft-delete

    # Step 4: Wait, verify nothing broke

    # Step 5: Hard-delete (permanent)
    ./schema-deletion-tool --from-file candidates.json --hard-delete

### Non-Interactive / CI Mode

Use `--force` for fully non-interactive execution. When `--force` is set:

- All clusters are scanned (no "select clusters to skip" prompt)
- Credentials must be provided via `--config-file` or `--cp-config-file` (no interactive prompts)
- Warned schemas are included in deletion without confirmation
- Soft-delete and hard-delete execute without confirmation

Examples:

    # Cloud: non-interactive soft-delete (requires config-file for credentials)
    ./schema-deletion-tool --all --soft-delete --force \
      --config-file creds.json --output deleted.json

    # CP: non-interactive dry-run
    ./schema-deletion-tool --platform cp --cp-config-file config.json \
      --all --dry-run --output candidates.json --scan-all-topics --force

    # Hard-delete from manifest without prompts
    ./schema-deletion-tool --from-file deleted.json --hard-delete --force

### Safety Checks

The tool automatically checks for the following before allowing deletion:

| Check | Status | Behavior |
|---|---|---|
| **Schema references** (`referencedby`) | `blocked_by_references` | Blocked: another active schema imports this one |
| **Migration rule chain** | `blocked_by_migration_chain` | Blocked: deleting breaks UPGRADE/DOWNGRADE path between active versions |
| **Encryption rules** (ENCRYPT/DECRYPT) | `blocked_by_encryption_rules` | Blocked: encrypted messages become unreadable |
| **Rule references** | `blocked_by_rule_reference` | Blocked: an active schema's rule references this version |
| **Domain rules** (CEL, transforms) | `has_domain_rules` | Warning: validation/transform enforcement will stop |
| **Migration rules** (non-chain-breaking) | `has_migration_rules` | Warning: migration rule will be lost |
| **Global rules inheritance** | Informational | Notes if subject inherits global default rules |
| **Transitive compatibility** | Informational | Warns about potential compatibility chain breakage |

Blocked schemas are skipped during deletion. Warned schemas prompt for confirmation (or are included with `--force`).

## All Flags

| Flag | Description | Default |
|---|---|---|
| `--subject`, `-V` | Subject to clean up | |
| `--all` | Process all eligible subjects | `false` |
| `--config-file` | Cloud cluster credentials JSON file | |
| `--platform` | Platform: `cloud` or `cp` | `cloud` |
| `--cp-config-file` | CP configuration JSON file | |
| `--strategy` | Naming strategy: `topic-name`, `record-name`, `topic-record-name` | `topic-name` |
| `--topics` | Comma-separated topics to scan | |
| `--scan-all-topics` | Scan all topics across clusters | `false` |
| `--context` | Schema context to scope to (omit for all contexts, `""` for default only) | all |
| `--dry-run` | Analyze without deleting | `false` |
| `--output` | Manifest output path (implies `--dry-run`) | |
| `--from-file` | Read manifest, skip scanning | |
| `--soft-delete` | Soft-delete only | `false` |
| `--hard-delete` | Hard-delete only | `false` |
| `--force` | Non-interactive mode: skip all prompts, scan all clusters, require credentials via config file | `false` |
| `--workers` | Number of concurrent topic scanners | `25` |

## How It Works

1. **List subjects** from Schema Registry (filtered by strategy and context)
2. **Resolve topics** to scan (from subjects, explicit list, or all topics)
3. **Scan messages** concurrently across clusters (25 workers by default) to identify active schema IDs from both magic byte prefix and message headers (`HeaderSchemaIdSerializer`)
4. **Run safety checks** (references, rules, migration chains, compatibility)
5. **Present results** with safe/blocked/warned categories
6. **Execute deletion** (soft-delete, then hard-delete with confirmation)

## License

See [LICENSE](LICENSE).
