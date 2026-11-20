# navalrelay-proxy

Proxy somente leitura da API do Docker usado pelo NavalRelay para diagnosticar o IPv6
(`/version`, `/info` e `/networks`). Imagem: `ghcr.io/clubenaval/navalrelay-proxy`.

- Base: `haproxy:3.4.7-alpine` com `apk upgrade` no build. A 3.4.8 trava o `docker exec` através do proxy; só atualize `HAPROXY_VERSION` depois de validar.
- Tudo bloqueado por padrão, exceto `VERSION` e `PING`. O `docker-compose.yml` libera também `INFO` e `NETWORKS`, com `POST=0`.
- Contêiner endurecido no compose: `read_only`, `tmpfs` em `/tmp` e `/run`, `cap_drop: [ALL]` e `no-new-privileges`.

Variáveis: cada seção da API (`CONTAINERS`, `IMAGES`, `NETWORKS`, `INFO`, `EVENTS`, `EXEC`, `POST`...) é liberada com `1`.

Baseado em [Tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy), licença Apache-2.0 (ver [`LICENSE`](LICENSE)). Modificações da Assessoria de TI do Clube Naval: versão do HAProxy fixada e `EVENTS` desligado por padrão.
