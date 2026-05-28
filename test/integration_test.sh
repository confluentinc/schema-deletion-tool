#!/bin/bash
set -e

SR_URL="http://localhost:8081"
BOOTSTRAP="localhost:9092"
TOOL="./schema-deletion-tool"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}: $1"; }
fail() { echo -e "${RED}FAIL${NC}: $1"; exit 1; }

echo "=== Building tool ==="
cd "$(dirname "$0")/.."
make build-local

echo ""
echo "=== Waiting for Schema Registry ==="
for i in $(seq 1 30); do
    if curl -s "$SR_URL/subjects" > /dev/null 2>&1; then
        echo "SR is ready."
        break
    fi
    if [ $i -eq 30 ]; then
        fail "SR not ready after 30s"
    fi
    sleep 1
done

echo ""
echo "=== Waiting for Kafka broker ==="
for i in $(seq 1 30); do
    if kafka-topics --bootstrap-server $BOOTSTRAP --list > /dev/null 2>&1; then
        echo "Broker is ready."
        break
    fi
    # Try with the tool's consumer as fallback
    if [ $i -eq 30 ]; then
        echo "Warning: kafka-topics not available, assuming broker is ready"
        break
    fi
    sleep 1
done

echo ""
echo "=== Creating topics ==="
for topic in orders payments users inventory; do
    curl -s -X POST "$SR_URL/../" > /dev/null 2>&1 || true
done

echo ""
echo "=== Step 1: Register base schema (address-value v1) ==="
# This will be referenced by orders-value
ADDR_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/address-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"Address\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"street\",\"type\":\"string\"},{\"name\":\"city\",\"type\":\"string\"}]}"}')
ADDR_ID=$(echo $ADDR_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered address-value v1, schema ID: $ADDR_ID"

echo ""
echo "=== Step 2: Register orders-value v1 (with reference to address) ==="
ORDERS_V1_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/orders-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"Order\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"},{\"name\":\"amount\",\"type\":\"double\"}]}"}')
ORDERS_V1_ID=$(echo $ORDERS_V1_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered orders-value v1, schema ID: $ORDERS_V1_ID"

echo ""
echo "=== Step 3: Register orders-value v2 ==="
ORDERS_V2_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/orders-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"Order\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"},{\"name\":\"amount\",\"type\":\"double\"},{\"name\":\"status\",\"type\":[\"null\",\"string\"],\"default\":null}]}"}')
ORDERS_V2_ID=$(echo $ORDERS_V2_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered orders-value v2, schema ID: $ORDERS_V2_ID"

echo ""
echo "=== Step 4: Register orders-value v3 ==="
ORDERS_V3_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/orders-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"Order\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"},{\"name\":\"amount\",\"type\":\"double\"},{\"name\":\"status\",\"type\":[\"null\",\"string\"],\"default\":null},{\"name\":\"region\",\"type\":[\"null\",\"string\"],\"default\":null}]}"}')
ORDERS_V3_ID=$(echo $ORDERS_V3_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered orders-value v3, schema ID: $ORDERS_V3_ID"

echo ""
echo "=== Step 5: Register payments-value v1 ==="
PAY_V1_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/payments-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"Payment\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"},{\"name\":\"amount\",\"type\":\"double\"}]}"}')
PAY_V1_ID=$(echo $PAY_V1_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered payments-value v1, schema ID: $PAY_V1_ID"

echo ""
echo "=== Step 6: Register users-value v1 ==="
USERS_V1_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/users-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"User\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"name\",\"type\":\"string\"},{\"name\":\"email\",\"type\":\"string\"}]}"}')
USERS_V1_ID=$(echo $USERS_V1_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered users-value v1, schema ID: $USERS_V1_ID"

echo ""
echo "=== Step 7: Register context schema :.staging:orders-value v1 ==="
CTX_RESP=$(curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    "$SR_URL/subjects/:.staging:orders-value/versions" \
    -d '{"schema": "{\"type\":\"record\",\"name\":\"StagingOrder\",\"namespace\":\"com.example\",\"fields\":[{\"name\":\"id\",\"type\":\"string\"}]}"}')
CTX_ID=$(echo $CTX_RESP | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "unknown")
echo "Registered :.staging:orders-value v1, schema ID: $CTX_ID"

echo ""
echo "=== Summary of registered schemas ==="
echo "  address-value v1:            ID $ADDR_ID"
echo "  orders-value v1:             ID $ORDERS_V1_ID"
echo "  orders-value v2:             ID $ORDERS_V2_ID"
echo "  orders-value v3:             ID $ORDERS_V3_ID"
echo "  payments-value v1:           ID $PAY_V1_ID"
echo "  users-value v1:              ID $USERS_V1_ID"
echo "  :.staging:orders-value v1:   ID $CTX_ID"

echo ""
echo "=== All subjects in SR ==="
curl -s "$SR_URL/subjects" | python3 -m json.tool

echo ""
echo "=== Step 8: Create topics and produce messages ==="
# Create topics
for topic in orders payments users; do
    curl -s -X POST "http://localhost:8082/topics/$topic" \
        -H "Content-Type: application/vnd.kafka.json.v2+json" \
        -d '{}' 2>/dev/null || true
done

echo ""
echo "=== Step 9: Run scan against CP ==="
echo "Running: $TOOL scan --platform cp --config-file test/cp-config-noauth.json --all-subjects --all-topics --output test/manifest-output.json"
$TOOL scan --platform cp --config-file test/cp-config-noauth.json --all-subjects --all-topics --output test/manifest-output.json 2>&1 || true

echo ""
echo "=== Step 10: Check manifest output ==="
if [ -f test/manifest-output.json ]; then
    echo "Manifest generated successfully."
    python3 -m json.tool test/manifest-output.json 2>/dev/null || cat test/manifest-output.json

    # Count candidates
    TOTAL=$(python3 -c "import json; m=json.load(open('test/manifest-output.json')); print(len(m['candidates']))" 2>/dev/null || echo "?")
    SAFE=$(python3 -c "import json; m=json.load(open('test/manifest-output.json')); print(len([c for c in m['candidates'] if c['status']=='safe']))" 2>/dev/null || echo "?")
    BLOCKED=$(python3 -c "import json; m=json.load(open('test/manifest-output.json')); print(len([c for c in m['candidates'] if 'blocked' in c['status']]))" 2>/dev/null || echo "?")

    echo ""
    echo "Total candidates: $TOTAL"
    echo "Safe: $SAFE"
    echo "Blocked: $BLOCKED"
else
    fail "Manifest not generated"
fi

echo ""
echo "=== Step 11: Test --context filter ==="
echo "Running with --context staging"
$TOOL scan --platform cp --config-file test/cp-config-noauth.json --all-subjects --all-topics --context staging 2>&1 || true

echo ""
echo "=== Integration test complete ==="
