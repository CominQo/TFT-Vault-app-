FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /out/server .

FROM alpine:3.20
RUN adduser -D app && mkdir /data && chown app /data
COPY --from=build /out/server /server
USER app
ENV PORT=8080 DATA_FILE=/data/data.json
VOLUME /data
EXPOSE 8080
CMD ["/server"]
