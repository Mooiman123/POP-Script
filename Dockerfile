FROM golang:1.20-alpine

WORKDIR /app

# Alleen go.mod kopiëren
COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o app .

CMD ["./app"]
