FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /fix-my-deshboard ./cmd/fixmydeshboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /fix-my-deshboard /fix-my-deshboard
EXPOSE 8080
ENTRYPOINT ["/fix-my-deshboard"]
