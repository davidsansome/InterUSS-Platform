FROM golang:latest AS build
ADD . /app
WORKDIR /app
RUN go mod download
RUN make

FROM alpine:latest
COPY --from=build /go/bin/grpc-backend /usr/bin
ENTRYPOINT ["/usr/bin/http-gateway"]