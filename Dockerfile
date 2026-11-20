FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/pnzj-nowpayments-sandbox .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/pnzj-nowpayments-sandbox .
ENV PORT=8787
EXPOSE 8787
ENTRYPOINT ["./pnzj-nowpayments-sandbox"]
