FROM golang:1.26-alpine

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

CMD ["go", "run", "./server/cmd/api"]
