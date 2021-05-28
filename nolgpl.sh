#!/bin/bash

# Look for any references to go-ethereum or EthAPIShim, outside of tests,
# except the loading of the module
LGPL=$(find cmd -name "*.go" | xargs egrep "github.com/go-ethereum|EthAPIShim" | grep -v "ethapiPlugin.Lookup")
if [ -n "$LGPL" ]; then
  echo "!!! go-ethereum imports outside of test files or ethbinding:";
  echo $LGPL;
  exit 1;
fi
