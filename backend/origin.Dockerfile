FROM golang:1.22-alpine AS build
WORKDIR /app
COPY . .
RUN go build -o origin ./origin

FROM alpine:3.19
WORKDIR /app
COPY --from=build /app/origin /app/origin
EXPOSE 8080
ENTRYPOINT ["/app/origin"]

