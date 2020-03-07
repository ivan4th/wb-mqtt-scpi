FROM golang:1.14.0
ADD . /wb-mqtt-scpi
WORKDIR /wb-mqtt-scpi
RUN go build

FROM ubuntu:bionic
ENV DEBIAN_FRONTEND noninteractive
COPY --from=0 /wb-mqtt-scpi/wb-mqtt-scpi /usr/local/bin

ENTRYPOINT ["/usr/local/bin/wb-mqtt-scpi"]
CMD ["-broker", "tcp://mosquitto:1883", "-debug", "-config", "/etc/wb-mqtt-scpi.conf"]
