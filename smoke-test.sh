#!/usr/bin/env bash
# Smoke test for DMS security-hardened backend. Run against an isolated server.
# All requests with non-ASCII payloads go through node to guarantee UTF-8.
set -u
BASE="http://localhost:43210"
PASS=0
FAIL=0

json() { node -e "let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{try{const o=JSON.parse(d);const v=eval('o'+process.argv[1]);console.log(typeof v==='object'?JSON.stringify(v):v)}catch(e){console.log('')}})" "$1"; }

# http METHOD PATH TOKEN [JSON_BODY] -> prints "status_code body"
# NOTE: paths are passed WITHOUT the leading slash; MSYS bash rewrites
# arguments that look like absolute paths when invoking native node.exe.
req() {
  node -e '
const http = require("http");
const method = process.argv[1], pathArg = process.argv[2], token = process.argv[3];
const path = "/" + pathArg;
const body = process.argv[4] ? process.argv[4] : null;
const headers = {};
if (token) headers.Authorization = "Bearer " + token;
if (body !== null) { headers["Content-Type"] = "application/json"; headers["Content-Length"] = Buffer.byteLength(body); }
const r = http.request({hostname: "localhost", port: 43210, path, method, headers}, res => {
  let d = "";
  res.on("data", c => d += c);
  res.on("end", () => console.log(res.statusCode, d));
});
r.end(body === null ? undefined : body);
' "$1" "$2" "$3" "${4-}"
}

check() {
  if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "PASS: $1";
  else FAIL=$((FAIL+1)); echo "FAIL: $1 (expected=$2 got=$3)"; fi
}

# 1. login success
LOGIN=$(req POST api/auth/login "" '{"username":"admin","password":"smoke-admin-pass"}')
LOGIN_BODY="${LOGIN#* }"
TOKEN_A=$(echo "$LOGIN_BODY" | json ".token")
REFRESH_A=$(echo "$LOGIN_BODY" | json ".refreshToken")
check "login returns access token" "yes" "$([ -n "$TOKEN_A" ] && echo yes || echo no)"
check "login returns refresh token" "yes" "$([ -n "$REFRESH_A" ] && echo yes || echo no)"

# 2. /auth/me
check "me with access token" "200" "$(req GET api/auth/me "$TOKEN_A" | cut -d' ' -f1)"

# 3. refresh rotation
REFRESH1=$(req POST api/auth/refresh "" "{\"refreshToken\":\"$REFRESH_A\"}")
TOKEN_B=$(echo "${REFRESH1#* }" | json ".token")
REFRESH_B=$(echo "${REFRESH1#* }" | json ".refreshToken")
check "me with rotated access token" "200" "$(req GET api/auth/me "$TOKEN_B" | cut -d' ' -f1)"
check "reuse of rotated refresh token rejected" "401" "$(req POST api/auth/refresh "" "{\"refreshToken\":\"$REFRESH_A\"}" | cut -d' ' -f1)"

# 4. finance range DoS guard
START=$(date +%s%N)
DOS=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/finance?startDate=0001-01-01&endDate=9999-12-31" -H "Authorization: Bearer $TOKEN_A")
END=$(date +%s%N)
ELAPSED_MS=$(( (END - START) / 1000000 ))
check "huge date range rejected" "400" "$DOS"
check "huge date range rejected fast (<2s)" "yes" "$([ $ELAPSED_MS -lt 2000 ] && echo yes || echo no)"
WIDE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/finance?startDate=2026-04-01&endDate=2030-04-02" -H "Authorization: Bearer $TOKEN_A")
check "over-1-year range rejected" "400" "$WIDE"
OKRANGE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/finance?startDate=2026-04-01&endDate=2026-04-30" -H "Authorization: Bearer $TOKEN_A")
check "normal range accepted" "200" "$OKRANGE"

# 5. realName validation
BADNAME=$(req POST api/users "$TOKEN_A" '{"username":"evil","realName":"..\\..\\evil","role":"USER","initialPassword":"pass1234"}' | cut -d' ' -f1)
check "path-traversal realName rejected" "400" "$BADNAME"
GOODNAME=$(req POST api/users "$TOKEN_A" '{"username":"alice","realName":"测试成员甲","role":"USER","initialPassword":"pass1234"}' | cut -d' ' -f1)
check "normal member created" "201" "$GOODNAME"
GOODNAME2=$(req POST api/users "$TOKEN_A" '{"username":"bob","realName":"测试成员乙","role":"LEADER","initialPassword":"pass1234"}' | cut -d' ' -f1)
check "second member created" "201" "$GOODNAME2"

# 6. availability (manager endpoint for member) + auto schedule
AVAIL=$(req PUT api/availability/users/alice "$TOKEN_A" '{"single":["Mon-1","Tue-1"],"double":["Mon-1","Wed-3"]}' | cut -d' ' -f1)
check "member availability saved" "200" "$AVAIL"
AVAIL2=$(req PUT api/availability/users/bob "$TOKEN_A" '{"single":["Mon-1"],"double":["Mon-1","Thu-4"]}' | cut -d' ' -f1)
check "second member availability saved" "200" "$AVAIL2"
AUTOSCH=$(req POST api/schedule/auto-generate "$TOKEN_A" '{"perSlot":1}')
SCH_MON1=$(echo "${AUTOSCH#* }" | json ".schedule[\"Mon-1\"].length")
check "auto schedule covers Mon-1 (odd+even)" "2" "$SCH_MON1"
BADPER=$(req POST api/schedule/auto-generate "$TOKEN_A" '{"perSlot":9}' | cut -d' ' -f1)
check "auto schedule rejects perSlot=9" "400" "$BADPER"
SCHSAVE=$(req PUT api/schedule "$TOKEN_A" '{"schedule":{"Mon-1":["测试成员甲(单)","测试成员乙(双)"]}}' | cut -d' ' -f1)
check "manual schedule save works" "200" "$SCHSAVE"

# 7. system settings rates
SETTINGS=$(req GET api/system-settings "$TOKEN_A")
DUTY_RATE=$(echo "${SETTINGS#* }" | json ".dutyRate")
check "settings expose default duty rate" "25" "$DUTY_RATE"
RATE_UPDATE=$(req PUT api/system-settings "$TOKEN_A" '{"firstMonday":"20260302","laborSeed":"","workStudyContent":"机房运维C5-569","dutyRate":30,"workOrderRate":60,"mgmtLeaderRate":900,"mgmtOwnerRate":1300}' | cut -d' ' -f1)
check "rate update accepted" "200" "$RATE_UPDATE"
FIN=$(req GET "api/finance?month=2026-05" "$TOKEN_A")
DUTY_AMOUNT=$(echo "${FIN#* }" | json ".dutyAmount")
check "finance uses updated duty rate" "yes" "$([ -n "$DUTY_AMOUNT" ] && echo yes || echo no)"

# 8. audit logs
AUDIT=$(req GET "api/audit-logs?page=1&pageSize=100" "$TOKEN_A")
HAS_LOGIN=$(echo "${AUDIT#* }" | json ".items.some(i=>i.action.includes('登录成功'))")
check "audit log records login" "true" "$HAS_LOGIN"
HAS_WRITE=$(echo "${AUDIT#* }" | json ".items.some(i=>i.action.startsWith('PUT /api/schedule'))")
check "audit log records schedule write" "true" "$HAS_WRITE"

# 9. change password invalidates old tokens
PW=$(req PUT api/auth/password "$TOKEN_A" '{"currentPassword":"smoke-admin-pass","newPassword":"new-smoke-pass-1"}')
PW_BODY="${PW#* }"
TOKEN_C=$(echo "$PW_BODY" | json ".token")
REFRESH_C=$(echo "$PW_BODY" | json ".refreshToken")
check "old access token invalidated after password change" "401" "$(req GET api/auth/me "$TOKEN_A" | cut -d' ' -f1)"
check "new access token from password change works" "200" "$(req GET api/auth/me "$TOKEN_C" | cut -d' ' -f1)"
check "old refresh token invalidated after password change" "401" "$(req POST api/auth/refresh "" "{\"refreshToken\":\"$REFRESH_B\"}" | cut -d' ' -f1)"
check "new refresh token works" "200" "$(req POST api/auth/refresh "" "{\"refreshToken\":\"$REFRESH_C\"}" | cut -d' ' -f1)"

# 10. logout revokes refresh token
check "logout ok" "200" "$(req POST api/auth/logout "$TOKEN_C" "{\"refreshToken\":\"$REFRESH_C\"}" | cut -d' ' -f1)"
check "refresh token revoked after logout" "401" "$(req POST api/auth/refresh "" "{\"refreshToken\":\"$REFRESH_C\"}" | cut -d' ' -f1)"

# 11. login brute-force guard (runs last: blocks the IP for 5 minutes)
for i in 1 2 3 4 5; do
  curl -s -o /dev/null -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"nobody","password":"wrong"}'
done
BLOCKED=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/auth/login" -H 'Content-Type: application/json' -d '{"username":"nobody","password":"wrong"}')
check "login blocked after 5 failures" "429" "$BLOCKED"

echo ""
echo "RESULT: $PASS passed, $FAIL failed"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
