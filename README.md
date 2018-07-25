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
Provides Key Management System (KMS) integration for signing transactions.

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

