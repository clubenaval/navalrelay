<div align="center">

# NavalRelay

**Relay SMTP com painel web: envie e-mails, acompanhe cada mensagem e mantenha SPF, DKIM e DMARC em ordem, tudo em um só lugar.**

[![Versão](https://img.shields.io/github/v/release/clubenaval/navalrelay?label=vers%C3%A3o&color=c8102e)](https://github.com/clubenaval/navalrelay/releases)
[![Imagem Docker](https://img.shields.io/badge/ghcr.io-navalrelay-c8102e)](https://github.com/clubenaval?tab=packages&repo_name=navalrelay)
![Go](https://img.shields.io/badge/Go-1.23-00add8)
![React](https://img.shields.io/badge/React-18-61dafb)
![Exim](https://img.shields.io/badge/Exim-4.96-6b7280)

<img src="docs/img/11-painel.png" alt="Painel do NavalRelay" width="900">

</div>

---

O **NavalRelay** combina um servidor de e-mail de saída (**Exim**) com um painel web moderno. Você configura tudo pelo navegador, sem editar arquivos do Exim, e acompanha o resultado de cada envio com relatórios, filtros e exportação em PDF.

## Sumário

- [Principais recursos](#principais-recursos)
- [Como funciona](#como-funciona)
- [Começando em 5 minutos](#começando-em-5-minutos)
- [Um passeio pelo painel](#um-passeio-pelo-painel)
- [Documentação](#documentação)
- [Atualizando](#atualizando)
- [Estrutura do projeto](#estrutura-do-projeto)
- [Roadmap](#roadmap)
- [Licença](#licença)
- [Licenças de terceiros](#licenças-de-terceiros)

## Principais recursos

| | |
|---|---|
| **Relay SMTP completo** | Entrega direta (Relay SMTP) ou por um provedor (Smarthost, como Gmail, Office 365 e SES). Portas 25 e 587, IPv4 e IPv6. |
| **SPF, DKIM e DMARC sem dor de cabeça** | Cadastre o domínio e a chave DKIM já nasce assinando as mensagens. O painel mostra os registros de DNS prontos para copiar e **verifica o DNS** para você. |
| **Relatórios de verdade** | Todas as mensagens, com remetente, destinatários, assunto, origem e resultado de entrega. Filtros, gráfico por dia, **exportação em CSV e PDF** (A4, tema claro). |
| **Vários servidores em um painel** | Um NavalRelay central recebe os eventos de outros relays (modo push, por HTTP ou HTTPS, atrás de proxy reverso). |
| **Integração com Mailcow** | Registra as mensagens recebidas e enviadas por servidores [Mailcow](https://mailcow.email) lendo a API, sem instalar nada neles. |
| **Autenticação forte** | Login local ou **LDAP/Active Directory**, 2FA por aplicativo (TOTP) ou e-mail, códigos de recuperação, dispositivo confiável por 30 dias e recuperação de senha por e-mail. |
| **Seguro por padrão** | Proteção contra *open relay*, redes autorizadas detectadas sozinhas, segredos criptografados (AES-256-GCM), limite de tentativas de login. |
| **Visual futurista** | Modo escuro e claro, menu recolhível, funciona no celular. |
| **Fácil de instalar** | Um `docker compose up -d`. Imagens públicas multi-arquitetura (amd64 e arm64). |

## Como funciona

```mermaid
flowchart LR
    A[Aplicações e servidores<br/>da sua rede] -->|SMTP 25 / 587| M[navalrelay-mta<br/>Exim 4.96]
    M -->|entrega direta + DKIM| D[(Internet)]
    M -.->|ou via smarthost| S[Gmail / Office 365 / SES]
    M -->|main.log| P[navalrelay-app<br/>painel + API em Go]
    P --> DB[(navalrelay-db<br/>MySQL)]
    P -->|gera a configuração| M
    MC[Servidores Mailcow] -->|API somente leitura| P
    R[Outros NavalRelay] -->|push HTTPS| P
    U[Você no navegador] -->|8080| P
```

- **`navalrelay-mta`**: o Exim. Recebe, assina com DKIM e entrega. A configuração é **gerada pelo painel** e recarregada sozinha.
- **`navalrelay-app`**: o painel e a API. Lê o log do Exim, grava no MySQL e gera a configuração do MTA.
- **`navalrelay-db`**: o MySQL, com mensagens, usuários e configurações.
- **`navalrelay-proxy`** (opcional): um proxy somente leitura da API do Docker, usado só para diagnosticar o IPv6.

## Qual docker-compose usar

O repositório traz **três** arquivos compose. Escolha um:

| Arquivo | Quando usar |
|---|---|
| `docker-compose.yml` | **Painel completo**: interface web, relatórios, usuários e DNS (app, MySQL, MTA). |
| `docker-compose.cli-relay.yml` | **Relay leve** sem painel e sem banco, com entrega direta e DKIM próprio. |
| `docker-compose.cli-smarthost.yml` | **Relay leve** sem painel e sem banco, entregando por um smarthost (Gmail, Office 365...). |

Os dois relays leves estão em [Relay CLI](docs/relay-cli.md); o painel, abaixo.

## Começando em 5 minutos

**1.** Salve o `docker-compose.yml` do repositório (ele usa só imagens públicas) e suba:

```bash
docker compose up -d
docker compose logs navalrelay-app | grep TOKEN
```

**2.** Abra `http://IP-DO-SERVIDOR:8080` e siga o **primeiro acesso** em duas etapas: token, usuário e e-mail; depois a senha.

<p align="center">
  <img src="docs/img/01-primeiro-acesso-1.png" alt="Primeiro acesso, etapa 1" width="48%">
  <img src="docs/img/02-primeiro-acesso-2.png" alt="Primeiro acesso, etapa 2" width="48%">
</p>

**3.** O **assistente** guia o resto: nome, fuso, modo de operação, redes autorizadas, domínios e DKIM, autenticação e logs.

**4.** Aponte suas aplicações para o servidor SMTP (porta 25 ou 587) usando o **IP da máquina**, nunca `localhost`. Veja [por quê](docs/docker-rede-ipv6.md#enviando-a-partir-do-próprio-servidor).

> **Troque a senha padrão `orbita` do banco** no `docker-compose.yml` antes de usar em produção e faça backup dos volumes `app-data` (contém a chave `master.key`) e `dbdata`.

Instalação detalhada, pastas mapeadas, Portainer e variáveis de ambiente: **[docs/instalacao.md](docs/instalacao.md)**.

## Um passeio pelo painel

### Painel e mensagens

<p align="center"><img src="docs/img/12-mensagens.png" alt="Lista de mensagens com filtros" width="900"></p>

Filtre por período, status, sentido, servidor, remetente, destinatário e origem. Clique em uma mensagem para ver cada tentativa de entrega.

<p align="center">
  <img src="docs/img/13-mensagem-detalhe.png" alt="Detalhes da mensagem" width="48%">
  <img src="docs/img/14-exportar-pdf.png" alt="Exportar PDF com filtros" width="48%">
</p>

### Relatório em PDF

Escolha o que entra no relatório e baixe um PDF A4, com tema claro, logo, resumo e gráfico por dia.

<p align="center"><img src="docs/img/43-relatorio-pdf.png" alt="Relatório em PDF" width="520"></p>

### Configuração guiada, em abas por finalidade

<p align="center">
  <img src="docs/img/22-config-entrega-dns.png" alt="Entrega e DNS: DKIM, SPF e DMARC" width="48%">
  <img src="docs/img/21-config-redes.png" alt="Redes e acesso" width="48%">
</p>

O painel monta os registros **SPF, DKIM e DMARC** do seu domínio, explica cada opção e mostra se já estão publicados no DNS.

### Assistente de configuração

<p align="center">
  <img src="docs/img/04-assistente-modo.png" alt="Assistente: modo de operação" width="32%">
  <img src="docs/img/05-assistente-redes.png" alt="Assistente: redes" width="32%">
  <img src="docs/img/06-assistente-entrega-dns.png" alt="Assistente: entrega e DNS" width="32%">
</p>

### Mailcow e vários servidores

<p align="center">
  <img src="docs/img/30-mailcow.png" alt="Servidores Mailcow" width="48%">
  <img src="docs/img/31-relays-remotos.png" alt="Relays remotos (modo central)" width="48%">
</p>

### Usuários, LDAP/AD e 2FA

<p align="center">
  <img src="docs/img/24-config-ldap.png" alt="Autenticação por LDAP/AD" width="48%">
  <img src="docs/img/33-seguranca-2fa.png" alt="2FA por aplicativo" width="48%">
</p>

<p align="center"><img src="docs/img/32-usuarios.png" alt="Usuários" width="900"></p>

### E-mails do sistema

Códigos de verificação, recuperação de senha e avisos usam um modelo HTML com a identidade visual do sistema.

<p align="center">
  <img src="docs/img/41-email-recuperacao.png" alt="E-mail de recuperação de senha" width="32%">
  <img src="docs/img/42-email-codigo-2fa.png" alt="E-mail com código de verificação" width="32%">
  <img src="docs/img/40-email-teste.png" alt="E-mail de teste" width="32%">
</p>

### Tema claro, menu recolhível e celular

<p align="center">
  <img src="docs/img/36-painel-claro.png" alt="Painel no tema claro" width="48%">
  <img src="docs/img/15-menu-recolhido.png" alt="Menu recolhido" width="48%">
</p>
<p align="center">
  <img src="docs/img/37-celular-painel.png" alt="Painel no celular" width="24%">
  <img src="docs/img/38-celular-menu.png" alt="Menu no celular" width="24%">
</p>

## Documentação

| Guia | O que cobre |
|---|---|
| **[Instalação](docs/instalacao.md)** | Docker Compose, pastas mapeadas, Portainer, variáveis de ambiente, backup e atualização. |
| **[Primeiro acesso e assistente](docs/primeiro-acesso-e-assistente.md)** | O que fazer depois de subir os contêineres. |
| **[Configurações](docs/configuracao.md)** | Cada aba das Configurações: modo Relay ou Smarthost, redes, e-mail do sistema, logs e retenção. |
| **[DNS: SPF, DKIM e DMARC](docs/dns-spf-dkim-dmarc.md)** | Domínios, chave DKIM, registros de DNS e verificação. |
| **[LDAP / Active Directory](docs/ldap-ad.md)** | Login com a conta do AD, grupos de acesso e de administradores, passo a passo e problemas comuns. |
| **[Segurança](docs/seguranca.md)** | 2FA, recuperação de senha, perfis, proteção contra *open relay* e criptografia. |
| **[Mensagens e relatórios](docs/relatorios.md)** | Painel, filtros, PDF, CSV e ocultar falhas. |
| **[Mailcow](docs/mailcow.md)** | Registrar as mensagens de servidores Mailcow. |
| **[Vários servidores](docs/multi-servidor.md)** | Servidor central e clientes. |
| **[Relay CLI](docs/relay-cli.md)** | Relay leve, sem painel e sem banco, em dois compose prontos: `docker-compose.cli-relay.yml` e `docker-compose.cli-smarthost.yml`. |
| **[Docker, rede e IPv6](docs/docker-rede-ipv6.md)** | Por que usar o IP da máquina, redes autorizadas e IPv6 no Docker. |
| **[Solução de problemas](docs/solucao-de-problemas.md)** | Logs do Exim, DKIM, DNS, Gmail e permissões. |

## Atualizando

As imagens públicas ficam em `ghcr.io/clubenaval/navalrelay-app` e `ghcr.io/clubenaval/navalrelay-mta` (e `navalrelay-cli` para o relay leve; uma tag por versão e `latest`). Para atualizar, troque a tag no `docker-compose.yml` (ou use `latest`) e rode:

```bash
docker compose pull && docker compose up -d
```

Atualize **as duas imagens** (app e mta) juntas. O banco é migrado automaticamente na inicialização, sem perda de dados. As notas de cada versão estão em [Releases](https://github.com/clubenaval/navalrelay/releases).

## Estrutura do projeto

```
backend/    API em Go: autenticação, 2FA, e-mails, configurações, domínios e DKIM,
            gerador de configuração do Exim, ingestão de logs, Mailcow, relatórios e PDF
frontend/   Painel em React + TypeScript (Vite), CSS próprio com modo escuro e claro
mta/        Imagem do Exim com o entrypoint que aplica a configuração, recarrega e gira os logs
docs/       Documentação e imagens
```

## Roadmap

- Fila em tempo real (ver, reenviar e remover) e rejeições SMTP (reject log)
- Alertas (Telegram, webhook, e-mail), monitor de blacklists e métricas Prometheus
- Leitor de relatórios DMARC e limite de envio por remetente
- Envio das mensagens de um Mailcow do cliente para o servidor central

## Licença

O NavalRelay é distribuído sob a [licença MIT](LICENSE).

## Licenças de terceiros

Os relatórios em PDF usam a fonte Liberation Sans (SIL Open Font License 1.1), embutida em `backend/fonts/`.

---

<div align="center">

Desenvolvido por **[Assessoria de TI](https://clubenaval.org.br)** - Clube Naval

</div>
