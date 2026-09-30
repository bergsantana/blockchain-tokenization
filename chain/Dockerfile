FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /node ./cmd/node && CGO_ENABLED=0 go build -o /seed ./cmd/seed

FROM alpine:3.20
COPY --from=build /node /node
COPY --from=build /seed /seed
