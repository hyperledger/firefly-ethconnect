#!/bin/sh

args=""
if [ "$#" -eq 0 ]; then
    if [ -n "${ETHCONNECT_CONFIGFILE}" ]; then
        args="server"
    else
        echo "ETHCONNECT_CONFIGFILE unset, and no commandline parameters. Running with default standalone configuration"
        args="rest -U http://127.0.0.1:8080 -I ./abis -r http://127.0.0.1:8545 -E ./events"
    fi
else
    args="$@"
fi

exec ethconnect $args