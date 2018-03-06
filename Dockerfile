FROM golang:1.9.3
RUN go get -u github.com/golang/dep/cmd/dep
WORKDIR /go/src/efs-broker
COPY . /go/src/efs-broker
RUN dep ensure -vendor-only
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /efs-broker ./cmd/main.go

FROM scratch
COPY --from=0 /efs-broker .
ENTRYPOINT ["/efs-broker"]
