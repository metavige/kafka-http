FROM alpine:3.4
ADD dist/main /

ENV PORT 8080

EXPOSE 8080
CMD ["/main"]
