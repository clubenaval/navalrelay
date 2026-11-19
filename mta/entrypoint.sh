#!/bin/bash
set -u

CONF_SRC=/config/exim.conf
CONF_DST=/etc/exim4/exim4.conf
PIDFILE=/run/exim4/exim.pid
APPLIED=""
EXIM_PID=""
LAST_ROTATE=0

log() { echo "[mta] $*"; }

mkdir -p /var/log/exim /run/exim4 /var/spool/exim4 /etc/exim4/tls /etc/exim4/dkim
chown -R Debian-exim:Debian-exim /var/log/exim /run/exim4 /var/spool/exim4
chmod 755 /var/log/exim

if [ ! -f /etc/exim4/tls/cert.pem ]; then
  log "gerando certificado TLS autoassinado"
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=$(hostname)" \
    -keyout /etc/exim4/tls/key.pem -out /etc/exim4/tls/cert.pem 2>/dev/null
fi
chown Debian-exim:Debian-exim /etc/exim4/tls/*.pem
chmod 640 /etc/exim4/tls/key.pem

apply_config() {
  local tmp=/etc/exim4/exim4.conf.new
  cp "$CONF_SRC" "$tmp" || return 1
  if ! exim4 -C "$tmp" -bV >/tmp/exim-check.log 2>&1; then
    log "configuração inválida, mantendo a anterior:"
    cat /tmp/exim-check.log
    rm -f "$tmp"
    return 1
  fi
  [ -f /config/logrotate.conf ] && install -o root -g root -m 644 /config/logrotate.conf /etc/logrotate-exim.conf
  mv "$tmp" "$CONF_DST"
  chmod 640 "$CONF_DST"
  chown root:Debian-exim "$CONF_DST"
  rm -rf /etc/exim4/dkim && mkdir -p /etc/exim4/dkim
  if [ -d /config/dkim ] && ls /config/dkim/*.key >/dev/null 2>&1; then
    cp /config/dkim/* /etc/exim4/dkim/
  fi
  chown -R root:Debian-exim /etc/exim4/dkim
  chmod 750 /etc/exim4/dkim
  chmod 640 /etc/exim4/dkim/* 2>/dev/null || true
  return 0
}

start_exim() {
  for f in main reject panic; do touch "/var/log/exim/$f.log"; done
  chown Debian-exim:Debian-exim /var/log/exim/*.log
  chmod 644 /var/log/exim/*.log
  exim4 -bdf -q1m &
  EXIM_PID=$!
  log "exim iniciado (pid $EXIM_PID)"
}

shutdown() {
  log "encerrando"
  [ -n "$EXIM_PID" ] && kill -TERM "$EXIM_PID" 2>/dev/null
  wait "$EXIM_PID" 2>/dev/null
  exit 0
}
trap shutdown TERM INT

log "aguardando configuração do NavalRelay"
while true; do
  v=$(cat /config/version 2>/dev/null || true)
  if [ -n "$v" ] && [ "$v" != "$APPLIED" ] && [ -f "$CONF_SRC" ]; then
    if apply_config; then
      APPLIED=$v
      if [ -n "$EXIM_PID" ] && kill -0 "$EXIM_PID" 2>/dev/null; then
        kill -HUP "$EXIM_PID" && log "configuração recarregada ($v)"
      else
        start_exim
      fi
    else
      APPLIED=$v
    fi
  fi

  if [ -n "$EXIM_PID" ] && ! kill -0 "$EXIM_PID" 2>/dev/null; then
    log "exim parou inesperadamente; reiniciando"
    start_exim
  fi

  chmod 755 /var/log/exim 2>/dev/null
  chmod a+r /var/log/exim/* 2>/dev/null

  now=$(date +%s)
  if [ $((now - LAST_ROTATE)) -ge 600 ] && [ -f /etc/logrotate-exim.conf ]; then
    logrotate -s /var/lib/logrotate-exim.status /etc/logrotate-exim.conf 2>&1 | sed 's/^/[logrotate] /'
    LAST_ROTATE=$now
  fi
  sleep 5 &
  wait $!
done
