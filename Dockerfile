FROM golang:1.27-bookworm AS build
WORKDIR /src
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o go-streaming .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
	ca-certificates \
	gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
	gstreamer1.0-plugins-bad gstreamer1.0-libav \
	&& rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /src/go-streaming .
EXPOSE 8085
ENTRYPOINT ["./go-streaming"]
