# Servidores Mailcow

O NavalRelay pode registrar as mensagens **recebidas e enviadas** por servidores [Mailcow](https://mailcow.email), lendo a API deles. Funciona em qualquer modo de implantação e **nada é instalado nem alterado no Mailcow**.

<p align="center"><img src="img/30-mailcow.png" alt="Servidores Mailcow" width="900"></p>

## Como conectar

1. No Mailcow, abra **Sistema > Configuração > Acesso > API** (em versões antigas: Configuração > Acesso).
2. Ative o acesso à API e, em **Permitir acesso à API a partir destes IPs/redes**, informe o **IP público do NavalRelay**. Sem isso o Mailcow responde "acesso negado".
3. Gere a **chave de API somente leitura**. **Nunca use a de leitura e escrita.**
4. No NavalRelay, abra **Mailcow** (menu **Fontes**), informe **nome**, **endereço** (`https://mail.exemplo.com.br`) e a **chave**, use **Testar conexão** e salve.

Se o certificado do Mailcow for autoassinado, marque **Não validar o certificado TLS**.

## Como funciona

- A cada **30 segundos** o NavalRelay consulta os logs `postfix` e `rspamd-history` do Mailcow (`GET /api/v1/get/logs/...`, cabeçalho `X-API-Key`) e **correlaciona** as duas fontes pelo ID de fila e pelo `Message-ID`.
- Cada mensagem aparece em **Mensagens**, com o nome do servidor, o **sentido** (↓ recebida, ↑ enviada), os destinatários e o resultado da entrega (entregue, adiada ou falha). Rejeições do Rspamd aparecem como falha.
- **Sentido**: mensagens autenticadas por um usuário do Mailcow contam como **enviadas**; as demais, como **recebidas**.
- O menu lateral mostra um **pontinho** ao lado de **Mailcow**: verde quando tudo sincroniza, vermelho quando algum servidor está com erro.

## Limites e observações

- O **histórico inicial** é limitado ao que o Mailcow mantém nos logs (até 10.000 linhas do Postfix e 1.000 entradas do Rspamd). Mensagens mais antigas aparecem como **Aceita** (o Rspamd aceitou, sem confirmação de entrega).
- **Avisos gerados pelo próprio Postfix** (atraso ou devolução, sem remetente) aparecem com o assunto "Notificação gerada pelo Postfix".
- Campos vazios mostram "Sem remetente", "Sem assunto", "Sem destinatário" e "Sem origem".
- A **chave de API** fica criptografada no banco.
- Se o servidor ficar indisponível, o erro aparece na lista e a coleta **continua de onde parou** quando ele voltar, desde que os logs do Mailcow ainda cubram o período.
- Para esconder as mensagens barradas pelo Rspamd, use **Ocultar rejeições do Rspamd** ([relatórios](relatorios.md#ocultar-falhas)).

## Mailcow em um relay remoto

As mensagens de um Mailcow ficam no banco de **quem o cadastrou**. Um relay em modo **Cliente** envia ao central apenas os eventos do Exim dele, **não** as mensagens do Mailcow. Para vê-las no central, cadastre o Mailcow no próprio central (ele precisa alcançar a API do Mailcow).
