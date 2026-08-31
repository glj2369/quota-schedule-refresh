FROM golang:1.25-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY . .
ENV CGO_ENABLED=1
RUN mkdir -p /out && go build -buildmode=c-shared -o /out/quota-schedule-refresh.so . && rm -f /out/quota-schedule-refresh.h
