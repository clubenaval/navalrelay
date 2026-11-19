# Solução de problemas

## Como ver os logs

### Logs do Exim (envio e recebimento)

O Exim grava em **arquivos**, não na saída padrão. Por isso `docker logs navalrelay-mta` mostra só as mensagens de inicialização (`[mta] …`).

```bash
# acompanhar em tempo real
docker exec -it navalrelay-mta tail -f /var/log/exim/main.log

# com pasta mapeada, direto no host
tail -f /srv/navalrelay/mta/logs/main.log
```

| Arquivo | Conteúdo |
|---|---|
| `main.log` | Entregas, recebimentos, adiamentos e falhas |
| `reject.log` | Conexões e mensagens recusadas (bots, *relay* negado) |
| `panic.log` | Erros graves de configuração |

Os três juntos:

```bash
docker exec -it navalrelay-mta sh -c 'tail -f /var/log/exim/main.log /var/log/exim/reject.log /var/log/exim/panic.log'
```

No `main.log`, `<=` é recebimento, `=>` é entrega, `**` é falha e `==` é adiamento.

```bash
# um destinatário ou remetente
docker exec navalrelay-mta grep "fulano@exemplo.com" /var/log/exim/main.log

# últimas falhas e adiadas
docker exec navalrelay-mta grep -E " \*\* | == " /var/log/exim/main.log | tail -20

# problemas de DKIM
docker exec navalrelay-mta grep -i dkim /var/log/exim/main.log | tail
```

Os arquivos antigos ficam compactados (`main.log.1`, `main.log.2.gz`…); use `zcat` para lê-los.

### Fila de mensagens

```bash
docker exec navalrelay-mta exim4 -bp          # lista a fila
docker exec navalrelay-mta exim4 -bpc         # quantas mensagens na fila
docker exec navalrelay-mta exim4 -Mvl <id>    # histórico de uma mensagem
```

### Logs do painel

```bash
docker logs -f navalrelay-app
```

## As mensagens saem sem assinatura DKIM

1. Confira se o domínio do **remetente** (cabeçalho `From`) está cadastrado em **Entrega e DNS** e com **DKIM ativo**.
2. No modo **Smarthost** a assinatura DKIM não é feita pelo relay.
3. Procure erros no log do MTA:

```bash
docker exec navalrelay-mta grep -i "dkim\|tainted" /var/log/exim/main.log /var/log/exim/panic.log | tail
```

4. Verifique se a chave e o mapa foram entregues ao Exim:

```bash
docker exec navalrelay-mta ls -la /etc/exim4/dkim
docker exec navalrelay-mta cat /etc/exim4/dkim/map
```

O mapa deve listar `seudominio.com.br: /etc/exim4/dkim/seudominio.com.br.key`. Se a mensagem `Tainted filename` aparecer, atualize para a versão mais recente (a correção está no mapa de chaves).

5. **Atualize as duas imagens** (app e mta) para a mesma versão.

## "Verificar DNS" não encontra os registros

- O painel consulta **resolvedores públicos** (`8.8.8.8` e `1.1.1.1`). O contêiner precisa de **saída UDP na porta 53** para a internet. Para usar outros, defina `ORBITA_DNS_SERVERS`.
- Confirme que o registro existe de fora da sua rede: `dig TXT dkim._domainkey.seudominio.com.br @8.8.8.8`.
- A propagação do DNS pode levar de minutos a horas.
- Se o **seletor** foi alterado e não foi salvo, o nome do registro no painel mostra um aviso.

## Smarthost do Google: erro `EOF`, `421` ou `535`

- **`autenticação SMTP recusada: EOF`** (ou `421 4.7.0`): o servidor fechou a conexão porque o EHLO era `localhost`. Atualize para a versão mais recente e confira se o **hostname do relay** está preenchido com um nome DNS válido.
- **`535`** ou **`550`**: autenticação ou remetente recusados. No `smtp-relay.gmail.com`, o **IP público do servidor** (ou o usuário) precisa estar autorizado no Google Workspace (Apps > Gmail > Roteamento > Serviço de relay de SMTP), e o remetente precisa ser de um domínio da conta.

Use o botão **Enviar teste pelo smarthost** em **Configurações > Smarthost** para ver o motivo na hora.

## `550 Connection through Docker gateway refused`

Você está enviando por `localhost` ou `127.0.0.1` na máquina do Docker. Use o **IP da máquina**. Veja [Docker, rede e IPv6](docker-rede-ipv6.md#enviando-a-partir-do-próprio-servidor).

## `open /data/master.key: permission denied` ou `open /var/log/exim/main.log: permission denied`

Acontece com **pastas mapeadas** criadas como `root`. As imagens atuais ajustam as permissões sozinhas ao iniciar. Se persistir, atualize **as duas imagens** e recrie os contêineres. Se você usa `--user`, o ajuste automático é ignorado e as pastas precisam ter a permissão correta.

## O painel mostra uma tela "Algo deu errado"

É a tela de segurança do painel quando algum componente falha ao renderizar. Recarregue a página e, se persistir, copie a mensagem mostrada e abra uma [issue](https://github.com/clubenaval/navalrelay/issues) com ela.

## Não consigo entrar (LDAP)

Veja [LDAP / Active Directory > Problemas comuns](ldap-ad.md#problemas-comuns).

## Esqueci a senha do administrador e não há e-mail configurado

Entre com outro administrador e use **Redefinir senha** em **Usuários**. Se não houver outro, é possível definir uma nova senha direto no banco (gerando um hash bcrypt), mas o mais simples é manter sempre uma segunda conta de administrador local.

## O e-mail do sistema (2FA, recuperação de senha) não chega

1. Em **Configurações > E-mail do sistema**, use **Enviar teste** e veja a mensagem de erro.
2. Se o SMTP for o próprio relay (`navalrelay-mta`), a rede Docker do app precisa estar em **Redes autorizadas** e o **remetente** deve ser de um domínio cadastrado (se "Restringir remetentes" estiver ligado).
3. Confira a caixa de spam.
4. Para a recuperação de senha, o **Endereço público do painel** precisa estar preenchido.

## Mensagens duplicadas ou faltando no painel

O painel lê o log do Exim de forma incremental e **não duplica** eventos. Se você trocou o volume de logs, o painel detecta a rotação pelo inode e retoma sozinho. As mensagens mais antigas que a **retenção** configurada são apagadas.
