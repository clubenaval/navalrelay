# Instalação

Este guia mostra como instalar o NavalRelay com Docker Compose, com pastas mapeadas ou pelo Portainer, e como atualizar e fazer backup. Para o relay leve, sem painel e sem banco, veja [Relay CLI](relay-cli.md).

## Requisitos

- Docker 24 ou mais novo e o plugin Docker Compose v2.
- Um servidor Linux (amd64 ou arm64) com as portas **25** e **587** (SMTP) e **8080** (painel) livres, ou outras que você escolher.
- Para entrega direta (modo Relay SMTP): um IP público com **DNS reverso (PTR)** e saída liberada na porta 25.

## Contêineres

| Serviço, contêiner e hostname | Função |
|---|---|
| `navalrelay-db` | MySQL |
| `navalrelay-app` | Painel e API (porta 8080) |
| `navalrelay-mta` | Exim (portas 25 e 587) |
| `navalrelay-proxy` | Proxy somente leitura da API do Docker (diagnóstico de IPv6) |

O nome do serviço, do contêiner e do hostname é o mesmo, então os comandos são equivalentes: `docker compose logs navalrelay-mta` ou `docker logs -f navalrelay-mta`, `docker exec -it navalrelay-app sh`.

## Instalação com Docker Compose

1. Baixe o [`docker-compose.yml`](../docker-compose.yml) do repositório. Ele usa só imagens públicas e valores literais, sem arquivo `.env`.
2. **Troque a senha do banco** (`MYSQL_PASSWORD` e `ORBITA_DB_PASSWORD`, que devem ser iguais).
3. Suba:

```bash
docker compose up -d
docker compose logs navalrelay-app | grep TOKEN
```

4. Acesse `http://IP-DO-SERVIDOR:8080` e siga o [primeiro acesso](primeiro-acesso-e-assistente.md).

A conexão com o MySQL (host, porta, banco, usuário, senha, SSL) fica nas variáveis `ORBITA_DB_*` do serviço `navalrelay-app`. Para usar um MySQL externo, altere `ORBITA_DB_HOST` e `ORBITA_DB_PORT` e remova o serviço `navalrelay-db` e o `depends_on`.

## Instalação pelo Portainer

1. Em **Stacks > Add stack**, escolha **Web editor** e dê um nome (por exemplo `navalrelay`).
2. Cole o conteúdo do `docker-compose.yml`, ajuste senhas, portas e volumes e clique em **Deploy the stack**.
3. Para atualizar depois, abra a stack e use **Update the stack** com **Re-pull image** ligado. Como alternativa, em **Stacks > Add stack > Repository**, informe a URL `https://github.com/clubenaval/navalrelay` e o caminho `docker-compose.yml`.

O compose do repositório cria os volumes nomeados `dbdata`, `app-data`, `mta-config`, `mta-logs`, `mta-spool` e `mta-tls`.

## Pastas mapeadas (bind mounts)

Em vez de volumes nomeados, você pode mapear diretórios do host:

```yaml
navalrelay-app:
  volumes:
    - /srv/navalrelay/data:/data
    - /srv/navalrelay/mta/config:/config
    - /srv/navalrelay/mta/logs:/var/log/exim:ro

navalrelay-mta:
  volumes:
    - /srv/navalrelay/mta/config:/config:ro
    - /srv/navalrelay/mta/logs:/var/log/exim
    - /srv/navalrelay/mta/spool:/var/spool/exim4
    - /srv/navalrelay/mta/tls:/etc/exim4/tls

navalrelay-db:
  volumes:
    - /srv/navalrelay/db:/var/lib/mysql
```

O Docker cria como `root` os diretórios que não existem. Para isso não causar `open /data/master.key: permission denied`, a imagem do app inicia como root, ajusta as permissões e então passa a rodar como o usuário `orbita` (uid 10001):

- `/data` e `/config` passam a pertencer ao usuário `orbita` (em `/config`, de forma recursiva).
- Em `/data`, só os arquivos da raiz (como `master.key`) são ajustados, **sem recursão**. Assim, subdiretórios de outros serviços dentro do mesmo caminho (como o `db` do MySQL e o `spool` do Exim) não são alterados.
- O MySQL e o Exim ajustam os próprios diretórios na inicialização.
- O Exim cria `main.log`, `reject.log` e `panic.log` com permissão de leitura para o app.

Não é preciso rodar `chown` manualmente. Se você iniciar o contêiner com `--user`, o ajuste é ignorado e o diretório precisa já estar com a permissão correta.

## Variáveis de ambiente (serviço `navalrelay-app`)

| Variável | Função |
|---|---|
| `ORBITA_DB_HOST`, `ORBITA_DB_PORT`, `ORBITA_DB_NAME`, `ORBITA_DB_USER`, `ORBITA_DB_PASSWORD`, `ORBITA_DB_SSL` | Conexão com o MySQL. `ORBITA_DB_SSL`: `disable`, `preferred`, `required` ou `skip-verify`. |
| `ORBITA_DOCKER_API` | Endereço do `navalrelay-proxy`, usado no diagnóstico de IPv6. |
| `ORBITA_HOST_IPS` | (opcional) IPs ou CIDRs da máquina, separados por vírgula, incluídos nas redes autorizadas. |
| `ORBITA_DOCKER_GATEWAYS` | (opcional) gateways do Docker a recusar, se a detecção automática não bastar. |
| `ORBITA_DNS_SERVERS` | (opcional) resolvedores usados para verificar os registros de DNS dos domínios, separados por vírgula (padrão `8.8.8.8,1.1.1.1`). |
| `ORBITA_LISTEN` | (opcional) endereço de escuta do painel (padrão `:8080`). |

No serviço `navalrelay-mta` use `TZ` para o fuso horário dos logs.

## Backup

| O quê | Onde | Por quê |
|---|---|---|
| **`app-data`** (`/data`) | contém a `master.key` | Os segredos (chaves DKIM, senhas de SMTP e LDAP, segredos de 2FA) são criptografados com ela. **Sem a `master.key` eles não podem ser recuperados.** |
| **`dbdata`** (`/var/lib/mysql`) | banco MySQL | Mensagens, usuários e configurações. Prefira `mysqldump` com o contêiner no ar. |
| `mta-config` (`/config`) | configuração gerada | Pode ser regenerada pelo painel. |

Exemplo de backup do banco:

```bash
docker exec navalrelay-db sh -c 'mysqldump -uorbita -p"$MYSQL_PASSWORD" orbita' > navalrelay-$(date +%F).sql
```

## Atualizando

As imagens públicas ficam em `ghcr.io/clubenaval/navalrelay-app`, `ghcr.io/clubenaval/navalrelay-mta` e `ghcr.io/clubenaval/navalrelay-proxy`, com uma tag por versão e `latest`. Troque a tag no compose (ou use `latest`) e rode:

```bash
docker compose pull && docker compose up -d
```

- Atualize **as duas imagens** (app e mta) juntas.
- O banco é migrado automaticamente na inicialização, sem perda de dados.
- Depois de atualizar, o app regrava a configuração do MTA e o Exim recarrega sozinho.

## Portas e firewall

| Porta | Uso |
|---|---|
| 25 | SMTP de envio das suas aplicações (e de recebimento de bounces, se aplicável) |
| 587 | Submissão |
| 8080 | Painel web (publique atrás de um proxy reverso com HTTPS em produção) |

Atrás de um proxy reverso com HTTPS, ative em **Configurações > Geral** a opção **Estou atrás de um proxy reverso**, para o painel registrar o IP real dos clientes e dos logins.
