FROM golang:1.12

WORKDIR /go/src/github.com/render-examples/go-web-server/
COPY . .
RUN go get github.com/gin-gonic/gin
RUN go get -d github.com/dustin/go-broadcast/...
RUN go get -d github.com/manucorporat/stats/...
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

# second build stage to keep the image small
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY resources resources
COPY --from=0 /go/src/github.com/render-examples/go-web-server/app .
CMD ["./app"]
