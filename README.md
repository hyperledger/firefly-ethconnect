# github.com/kaleido-io/ethconnect

[![Coverage report](https://codecov.io/gh/kaleido-io/ethconnect/branch/master/graph/badge.svg)](https://codecov.io/gh/kaleido-io/ethconnect)

Open Source component that is used as part of [Kaleido Connect](https://kaleido.io).

A Web and Messaging API, taking the hassle out of submitting Ethereum transactions:
- Solidity compilation
- ABI type mapping
- RLP encoding
- Transaction receipt polling
- High throughput tx submission
- Concurrency management
- Nonce management

Provides an integration bridge into Ethereum permissioned chains, from simple
Web Service and Messaging interfaces that are friendly to existing Enterprise
applications & middleware.
For example to allow connectivity from an Enterprise Service Bus (ESB) or other
Enterprise Application Integration (EAI) tier, or applications running in a
Java EE Application Server.

![kaleido-io/ethconnect](ethconnect.png)

Technology support currently includes:
- Messaging
  - Apache Kafka - https://kafka.apache.org/
- Webhooks
  - Simple POST of a transaction over HTTP in YAML/JSON queued to Kafka for processing

Under development:

- Key Management Service
  - AWS Key Management Service - https://aws.amazon.com/kms/

## License

This code is distributed under the [Apache 2 license](LICENSE).

> The code statically links to code distributed under the LGPL license.

## Example payloads

YAML to deploy a contract

```yaml
headers:
  type: DeployContract
from: '0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8'
params:
  - 12345
gas: 1000000
solidity: |-
  pragma solidity ^0.4.17;
  
  contract simplestorage {
     uint public storedData;
  
     function simplestorage(uint initVal) public {
        storedData = initVal;
     }
  
     function set(uint x) public {
        storedData = x;
     }
  
     function get() public constant returns (uint retVal) {
        return storedData;
     }
  }
```

YAML to submit a transaction
```yaml
headers:
  type: SendTransaction
from: 0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8
to: 0xe1a078b9e2b145d0a7387f09277c6ae1d9470771
params:
  - value: 4276993775
    type: uint256
gas: 1000000
methodName: set
```
