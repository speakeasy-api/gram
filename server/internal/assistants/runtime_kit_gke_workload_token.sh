set -eu
umask 077
url='http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/identity'
while :; do
  if curl -sfG --max-time 10 --max-filesize 16384 -H 'Metadata-Flavor: Google' \
    --data-urlencode "audience=$WORKLOAD_TOKEN_AUDIENCE" --data-urlencode 'format=full' \
    -o "$WORKLOAD_TOKEN_PATH.next" "$url"; then
    mv -f "$WORKLOAD_TOKEN_PATH.next" "$WORKLOAD_TOKEN_PATH"
    sleep 300
  else
    echo 'workload token refresh failed; retrying' >&2
    rm -f "$WORKLOAD_TOKEN_PATH.next"
    find "$WORKLOAD_TOKEN_PATH" -mmin +50 -delete 2>/dev/null || true
    sleep 5
  fi
done
