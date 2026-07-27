#!/usr/bin/env bash
#
# End-to-end smoke test for the new NGN + KYC-intake flow.
#
# Creates a test user against a running DigitalFX instance, authenticates them,
# and walks every step:
#   1. Register an individual (mailinator email).
#   2. Login → JWT.
#   3. GET  /kyc/requirements       — the fields we collect before Sumsub.
#   4. POST /kyc/init (pre-intake)  — MUST be rejected with KYC_INTAKE_REQUIRED.
#   5. POST /kyc/intake             — submit our own fields (incl. BVN for NGN).
#   6. POST /kyc/init (post-intake) — now returns a Sumsub SDK token.
#   7. GET  /wallets/overview       — assert an NGN (Naira) wallet exists.
#
# Steps 6 needs live Sumsub creds; step 7's real Nomba bank account number is
# filled in only after KYC is approved (admin or Sumsub webhook), which triggers
# Nomba provisioning. Everything else works without external providers.
#
# Usage:
#   BASE_URL=https://digitalcapfx-....run.app ./scripts/e2e_kyc_ngn_flow.sh
#
# Requires: curl, jq. The OTP email lands in the mailinator public inbox:
#   https://www.mailinator.com/v4/public/inboxes.jsp?to=<inbox>
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
API="$BASE_URL/api/v1"
STAMP="$(date +%s)"
INBOX="digitalfx_e2e_${STAMP}"
EMAIL="${INBOX}@mailinator.com"
# Unique-ish Cameroon E.164 number for the test user.
PHONE="+23767${STAMP:5:6}"
PIN="123456"

pass() { echo "  ✅ $*"; }
fail() { echo "  ❌ $*"; exit 1; }
hr()   { echo "──────────────────────────────────────────────────────────────"; }

echo "Base:  $API"
echo "Email: $EMAIL"
echo "Phone: $PHONE"
hr

# 1. Register ------------------------------------------------------------------
echo "1) Register individual"
REG=$(curl -s -X POST "$API/auth/register" -H 'Content-Type: application/json' -d "{
  \"account_type\":\"individual\",
  \"phone\":\"$PHONE\",\"email\":\"$EMAIL\",
  \"first_name\":\"Ada\",\"last_name\":\"Lovelace\",
  \"pin\":\"$PIN\",\"country\":\"NG\"
}")
ACCESS=$(echo "$REG" | jq -r '.data.access_token // .access_token // empty')
[ -n "$ACCESS" ] || ACCESS=$(echo "$REG" | jq -r '.data.tokens.access_token // empty')
if [ -z "$ACCESS" ]; then
  # Some deployments require login after register.
  LOGIN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' -d "{\"phone\":\"$PHONE\",\"pin\":\"$PIN\"}")
  ACCESS=$(echo "$LOGIN" | jq -r '.data.access_token // .access_token // empty')
fi
[ -n "$ACCESS" ] && pass "registered + got JWT" || fail "no access token: $REG"
AUTH=(-H "Authorization: Bearer $ACCESS")
hr

# 2. KYC requirements ----------------------------------------------------------
echo "2) GET /kyc/requirements"
REQ=$(curl -s "$API/kyc/requirements" "${AUTH[@]}")
echo "$REQ" | jq -e '.data.fields | map(.key) | index("bvn")' >/dev/null \
  && pass "intake schema includes bvn (for the Naira account)" \
  || fail "requirements missing bvn: $REQ"
echo "$REQ" | jq -r '.data.account_type, (.data.completed|tostring)' | tr '\n' ' '; echo
hr

# 3. KYC init BEFORE intake — must be gated -----------------------------------
echo "3) POST /kyc/init before intake (expect KYC_INTAKE_REQUIRED)"
CODE=$(curl -s -o /tmp/kyc_init_pre.json -w '%{http_code}' -X POST "$API/kyc/init" "${AUTH[@]}")
ERRCODE=$(jq -r '.error.code // .code // empty' /tmp/kyc_init_pre.json 2>/dev/null)
if [ "$CODE" = "400" ] && [ "$ERRCODE" = "KYC_INTAKE_REQUIRED" ]; then
  pass "Sumsub correctly gated until intake is completed"
else
  fail "expected 400 KYC_INTAKE_REQUIRED, got $CODE / $ERRCODE ($(cat /tmp/kyc_init_pre.json))"
fi
hr

# 4. Submit intake -------------------------------------------------------------
echo "4) POST /kyc/intake"
INTAKE=$(curl -s -X POST "$API/kyc/intake" "${AUTH[@]}" -H 'Content-Type: application/json' -d '{
  "legal_first_name":"Ada","legal_last_name":"Lovelace",
  "date_of_birth":"1990-12-10","nationality":"NG",
  "bvn":"12345678901",
  "address_line1":"1 Marina Rd","city":"Lagos","state":"Lagos","postal_code":"100001","country":"NG",
  "occupation":"Engineer","source_of_funds":"Salary","purpose_of_account":"Personal Use"
}')
echo "$INTAKE" | jq -e '.data.status == "completed"' >/dev/null \
  && pass "intake stored + marked completed" \
  || fail "intake not completed: $INTAKE"
hr

# 5. KYC init AFTER intake -----------------------------------------------------
echo "5) POST /kyc/init after intake (expect a token, or a provider error if Sumsub creds absent)"
INIT=$(curl -s -X POST "$API/kyc/init" "${AUTH[@]}")
TOKEN=$(echo "$INIT" | jq -r '.data.token // .token // empty')
if [ -n "$TOKEN" ]; then
  pass "Sumsub SDK token issued (intake gate passed)"
else
  echo "  ⚠️  no token — expected only if SUMSUB_* creds are unset on the server: $INIT"
fi
hr

# 6. Wallets overview — NGN present -------------------------------------------
echo "6) GET /wallets/overview (assert NGN wallet)"
OV=$(curl -s "$API/wallets/overview" "${AUTH[@]}")
if echo "$OV" | jq -e '.. | .symbol? // .currency? // empty | select(. == "NGN")' >/dev/null 2>&1; then
  pass "NGN (Naira) wallet present for the user"
else
  echo "  ⚠️  NGN not found in overview (check response): $(echo "$OV" | jq -c '.data // .' | head -c 400)"
fi
hr
echo "Done. Inbox for OTP/email checks: https://www.mailinator.com/v4/public/inboxes.jsp?to=$INBOX"
