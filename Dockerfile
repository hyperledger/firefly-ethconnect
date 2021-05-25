FROM golang:1.16.4-alpine3.13 AS builder
WORKDIR /ethconnect
ADD . .
RUN apk add make gcc build-base
RUN make

FROM alpine:latest
WORKDIR /ethconnect
COPY --from=builder /ethconnect/ethconnect .
RUN ln -s /ethconnect/ethconnect /usr/bin/ethconnect
RUN mkdir abis
ENTRYPOINT [ "ethconnect", "rest", "-U", "http://127.0.0.1:8080", "-I", "./abis", "-r", "http://127.0.0.1:8545", "-E", "./events" ]