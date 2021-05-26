FROM golang:1.16.4-alpine3.13 AS builder
WORKDIR /ethconnect
ADD . .
RUN apk add make gcc build-base
RUN make

FROM alpine:latest
WORKDIR /ethconnect
COPY --from=builder /ethconnect/ethconnect .
COPY --from=builder /ethconnect/start.sh .
RUN ln -s /ethconnect/ethconnect /usr/bin/ethconnect
RUN mkdir abis
ENTRYPOINT [ "./start.sh" ]
