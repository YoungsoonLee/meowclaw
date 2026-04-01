#!/bin/sh
set -e
export HOME="${HOME:-/data}"
cfgdir="${HOME}/.meowclaw"
mkdir -p "$cfgdir"
chmod 700 "$cfgdir" 2>/dev/null || true

if [ ! -f "$cfgdir/config.yaml" ]; then
	cp /usr/local/share/meowclaw/config.docker.yaml "$cfgdir/config.yaml"
	chmod 600 "$cfgdir/config.yaml" 2>/dev/null || true
	echo "meowclaw: created $cfgdir/config.yaml — enable channels, set API keys (or use MEOWCLAW_* env), then restart if needed."
fi

exec /usr/local/bin/meowclaw up "$@"
