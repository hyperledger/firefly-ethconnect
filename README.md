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

[![kaleido-io/ethconnect](ethconnect.png)](ethconnect.pdf)

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

The HTTP Webhooks and Kafka message payloads share a common schema.
For HTTP you can specify `Content-type: application/x-yaml` and send
in a YAML payload, which will be converted to JSON.

When sending messages to Kafka directly, JSON must be sent.

### YAML to submit a transaction

Send a transaction with parameters to an existing deployed contract.

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

### YAML to deploy a contract

Ideal for deployment of simple contracts that can be specified inline (see #18).

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

## Why put a Web / Messaging API in front of an Ethereum node?

The JSON/RPC specification exposed natively by Go-ethereum and other Ethereum
protocol implementations, provides a rich low-level API for interfacing with
the node. It is usually exposed over HTTP (as well as IPC) so can be connected
over a network, and have security layered in front of it (as has been down within
the Kaleido platform).

However, applications seldom code directly to the JSON/RPC API when deploying
contracts and sending transactions, because it is:
- Asynchronous in nature, needing polling to obtain a transaction receipt
- Based on Recursive Length Prefix (RLP) encoding of payloads

Instead thick client libraries such as [web3.js](https://github.com/ethereum/web3.js/) (web3j)[https://github.com/web3j/web3j], (web3.py)[https://github.com/ethereum/web3.py], (Nethereum)[https://github.com/Nethereum/Nethereum] and [ethjs](https://github.com/ethjs/ethjs) are used to submit transactions.

These thick client libraries perform many of the same functions as kaleido-io/ethconnect, simplifying submission of transactions, receipt checking,
nonce management etc.

In the modern world of Microservice architectures, having a simple, efficient 
and stateless REST API to submit transactions is a desirable alternative. A small self-contained, optimized, and independently scalable layer that can exposing a simple API that can be consumed by any application, programming language, or Integration tool.

e.g. making it trivial to submit transactions. No coding required, just open up Postman and send in a trivial piece of YAML copied from a README like this one.

In an Enterprise context, the availability of simple standardized interfaces like HTTP and Kafka means applications and integration tools (a common ESB or other EAI tooling) can be hooked up trivially. No need to insert complex code libraries into applications,
many of which are LGPL licensed.

## Why Messaging?

So you ask, if the goal is simplicity, why not just put the simple HTTP API in front of JSON/RPC and be done with it?

There are some challenges in Enterprise grade Blockchain solutions (particularly in high throughput permissioned/private chains) that cannot be solved by a stateless HTTP bridging layer alone.

So for Kaleido, we started with a robust Messaging tier and layered the HTTP interface on top.

## The asynchronous nature of Ethereum transactions

Ethereum transactions can take many seconds or minutes (depending on the backlog and block period) from submission until they make it into a block, and a receipt is available. Each individual node in Ethereum provides a built-in simple pool where transactions can be pooled while waiting to enter a block.

Connecting a synchronous blocking HTTP interface directly to an inherently asynchronous system like this can cause problems. The HTTP requester times out waiting for a response, but has no way to know if it should retry or if the transaction will eventually succeed. It cannot cancel the request.

Providing a Messaging layer with at-least-once delivery and message ordering, allows the asynchronous nature of Ethereum to be reflected back to the remote application.

Applications that have their own state stores are able to communicate over Messaging / Kafka with trivially simple JSON payloads to stream transactions into a scalable set of Ethereum nodes, and process the replies as they occur. The application can scale horizontally. Applications can also be decoupled from the Ethereum network with an integration technology like an Enterprise Service Bus (ESB).

When spikes in workload occur that create a large queue of transactions that need to be fed into the Ethereum network at a lower rate, the kaleido-io/ethconnect bridge feeds them in at an optimal rate.

### Ethereum Webhooks and the REST Receipt Store (MongoDB)

Another key goal of having a robust Messaging layer under the covers is that the most trivial application can send messages into Ethereum reliably.

- `POST` a [trivial YAML/JSON payload](#yaml-to-submit-a-transaction)
  - to `/hook` to complete once the message is confirmed by Kafka (0.5s - tunable)
  - to `/fasthook` to complete immediately when the message is sent to Kafka
- Get an _immediate_ receipt
   - No need to wait many seconds for a block to be cut
   - No need to have complex retry handling

This is ideal for Applications, Integration-as-a-Service (IaaS) platforms, and built-in SaaS integrations that support Webhooks for event submission.

However, SaaS administrators and end-users still need to be able to track down what happened for a particular transaction, to diagnose problems, and correlate details of the Ethereum transaction with the business event.

So kaleido-io/ethereum comes with a built-in receipt store, backed by MongoDB. It listens reliably for the replies over Kafka, and inserts each of them into the Database.

It provides a trivially simple REST API:
- `GET` `/reply/a789940d-710b-489f-477f-dc9aaa0aef77` to look for an individual reply
- `GET` `/replies` to list the replies
  - Ordered by time _received_ (not the order submitted) - listing the newest first
  - `limit` and `skip` query parameters can be used to paginate the results

A capped collection can be used in MongoDB to limit the storage. For example to store only the last 1000 replies received.

### Scale and message ordering - nonce management

The transaction pooling/execution logic within an Ethereum node is based upon the concept of a `nonce`, which must be incremented exactly once each time a transaction is submitted from the same Ethereum address. There can be no gaps in the nonce values, or messages build up in the `queued transaction` pool waiting for the gap to be filled. This allows for deterministic ordering of transactions sent by the same sender.

The management of this `nonce` pushes complexity back to the application tier - especially for horizontally scaled Enterprise applications sending many transactions using the same sender. By using an ordered Messaging stream to submit messages into the Ethereum network, many applications are able to delegate this complexity to kaleido-io/ethconnect.

The kaleido-io/ethconnect bridge contains all the logic necessary to communicate with the node to determine the next nonce, and also to cope with multiple requests being in flight within the same block, for the same sender (including [with IBFT](https://github.com/ethereum/EIPs/issues/650#issuecomment-360085474)).

If a sender needs to achieve exactly-once delivery of transactions (vs. at-least-once) it is still necessary to allocate the nonce within the application and pass it into kaleido-io/ethconnect in the payload.  This allows the sender to control allocation of nonces using its internal state store / locking.

Given many Enterprise scenarios involve writing hashed proofs of completion of an off-chain transaction to a shared ledger (vs. performing the actual transaction on the chain), at-least-once delivery is sufficient in a wide range of cases. Smart Contracts can simply re-store the proof.

## Why Kafka?

We selected Kafka 
