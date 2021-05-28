FROM golang:1.16-buster AS builder
WORKDIR /ethconnect
ADD . .
RUN apt-get update -y \
 && apt-get install -y build-essential git \
 && curl -Lo /usr/bin/solc https://github.com/ethereum/solidity/releases/download/v0.7.6/solc-static-linux \
 && chmod 755 /usr/bin/solc \
 && mv go.mod go.mod.orig \
 && grep -v ethbinding go.mod.orig > go.mod \
 && go get github.com/kaleido-io/ethbinding \
 && make clean all

FROM debian:buster-slim
WORKDIR /ethconnect
COPY --from=builder /ethconnect/ethconnect .
COPY --from=builder /ethconnect/ethbinding.so .
COPY --from=builder /ethconnect/start.sh .
RUN ln -s /ethconnect/ethconnect /usr/bin/ethconnect
RUN mkdir abis
ENTRYPOINT [ "./start.sh" ]
