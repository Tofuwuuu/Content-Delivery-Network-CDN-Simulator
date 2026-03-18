FROM golang:1.22-alpine AS build
WORKDIR /app
COPY . .
RUN go build -o edge ./edge

FROM alpine:3.19
WORKDIR /app
COPY --from=build /app/edge /app/edge
EXPOSE 8081
ENTRYPOINT ["/app/edge"]

