FROM ethereum/solc:0.8.30 AS compiler

FROM ghcr.io/foundry-rs/foundry:v1.7.1
COPY --from=compiler /usr/bin/solc /usr/local/bin/solc
ENTRYPOINT ["forge"]
