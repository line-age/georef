#!/bin/sh
set -e

# Percent-encode a string so it is safe inside a URL userinfo component.
# Encodes every byte that is not an unreserved URI character (RFC 3986 §2.3).
urlencode() {
    printf '%s' "$1" | awk 'BEGIN{for(i=0;i<256;i++)ord[sprintf("%c",i)]=i}
    {for(i=1;i<=length($0);i++){c=substr($0,i,1);
     if(c~/[A-Za-z0-9._~-]/){printf "%s",c}else{printf "%%%02X",ord[c]}}}'
}

DB_USER_ENC=$(urlencode "$DB_USER")
DB_PASS_ENC=$(urlencode "$DB_PASSWORD")
DATABASE_URL="postgres://${DB_USER_ENC}:${DB_PASS_ENC}@${DB_HOST}:${DB_PORT:-5432}/${DB_NAME}?sslmode=${DB_SSLMODE:-disable}"
migrate -path "$APP_HOME/migrations" -database "$DATABASE_URL" up
exec /usr/local/bin/server
