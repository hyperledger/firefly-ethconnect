#!/bin/sh

if [ "$#" -eq 0 ]; then
    if [ -n "${ETHCONNECT_CONFIGFILE}" ]; then
        ethconnect server
    else
        echo "ETHCONNECT_CONFIGFILE unset, and no commandline parameters. Running with default standalone configuration"
        ethconnect rest -U http://127.0.0.1:8080 -I ./abis -r http://127.0.0.1:8545 -E ./events
    fi
else
    ethconnect $@
fi