#!/bin/sh
set -e

if command -v Xvfb > /dev/null 2>&1; then
    Xvfb :99 -screen 0 1280x800x24 -nolisten tcp &
    sleep 1
    x11vnc -display :99 -nopw -listen localhost -xkb -forever -shared -quiet &
fi

exec /app/jobifai "$@"
