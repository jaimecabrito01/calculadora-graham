#!/usr/bin/env bash
# Deploy da calculadora-graham via Docker Compose no runner self-hosted.
#
# Fluxo: extrai o commit rastreado para releases/<sha>, gera backend/.env a partir
# das variáveis de ambiente (GitHub Secrets), constrói com tag = SHA, sobe com
# healthcheck e, em falha, volta para a release anterior.
#
# Obrigatórios: SHA, BRAPI_API_KEY. Opcionais: DEPLOY_DIR (default ~/calculadora-graham),
# POSTGRES_PASSWORD (default "graham" — só tem efeito em volume novo),
# BRAPI_URL, COMPOSE_PROJECT_NAME (default calculadora-graham), DRY_RUN=1.
set -Eeuo pipefail

: "${SHA:?SHA obrigatório (github.sha)}"
: "${BRAPI_API_KEY:?BRAPI_API_KEY obrigatório — configure o secret no environment 'deployment'}"

DEPLOY_DIR="${DEPLOY_DIR:-$HOME/calculadora-graham}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-calculadora-graham}"
RELEASES="$DEPLOY_DIR/releases"
REL="$RELEASES/$SHA"
CURRENT="$DEPLOY_DIR/current"
KEEP_RELEASES=5

export COMPOSE_PROJECT_NAME
export IMAGE_TAG="$SHA"
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-graham}"

log() { printf '[deploy] %s\n' "$*"; }

# "docker compose" sempre a partir da release e com o nome de projeto fixo,
# para reaproveitar os volumes pgdata/cvmdata existentes.
dc() {
  ( cd "$REL" && docker compose -f docker-compose.yml "$@" )
}

write_env() {
  umask 077
  {
    printf 'BRAPI_API_KEY=%s\n' "$BRAPI_API_KEY"
    printf 'PORT=8080\n'
    if [ -n "${BRAPI_URL:-}" ]; then
      printf 'BRAPI_URL=%s\n' "$BRAPI_URL"
    fi
  } > "$REL/backend/.env"
  chmod 600 "$REL/backend/.env"
}

healthcheck() {
  local tentativas=20
  for _ in $(seq 1 "$tentativas"); do
    if curl -fsS --max-time 3 http://127.0.0.1:8090/healthz >/dev/null 2>&1 \
       && curl -fsS --max-time 3 http://127.0.0.1:5174/ >/dev/null 2>&1; then
      return 0
    fi
    sleep 3
  done
  return 1
}

rollback() {
  local prev="$1"
  if [ -z "$prev" ] || [ ! -f "$RELEASES/$prev/docker-compose.yml" ]; then
    log "sem release anterior para rollback (primeiro deploy) — containers atuais preservados"
    return 0
  fi
  log "rollback para $prev"
  local prev_rel="$RELEASES/$prev"
  if ! ( cd "$prev_rel" && IMAGE_TAG="$prev" docker compose -f docker-compose.yml up -d --no-build --wait --wait-timeout 180 ); then
    log "ERRO: rollback para $prev também falhou — intervenção manual necessária"
    return 1
  fi
  ln -sfn "$prev_rel" "$CURRENT"
  log "rollback concluído em $prev"
}

# --- extrai a release a partir do commit (somente arquivos rastreados) ---
mkdir -p "$REL"
if [ -n "$(ls -A "$REL" 2>/dev/null)" ]; then
  # release já existe (retry do mesmo SHA): reextrai por cima
  find "$REL" -mindepth 1 -delete
fi
git archive HEAD | tar -x -C "$REL"
write_env
log "release $SHA extraída em $REL"

if [ "${DRY_RUN:-0}" = "1" ]; then
  log "DRY_RUN=1: release gerada, sem build/deploy"
  exit 0
fi

# --- tag da release atualmente publicada (para rollback) ---
PREV=""
if [ -f "$DEPLOY_DIR/last_good_tag" ]; then
  PREV="$(cat "$DEPLOY_DIR/last_good_tag")"
fi

# --- build e deploy ---
falhou=0
if ! dc build; then
  log "ERRO: build falhou"
  falhou=1
fi
if [ "$falhou" -eq 0 ] && ! dc up -d --no-build --wait --wait-timeout 240; then
  log "ERRO: subida dos containers falhou"
  falhou=1
fi
if [ "$falhou" -eq 0 ] && ! healthcheck; then
  log "ERRO: healthcheck não respondeu (API :8090/healthz ou front :5174)"
  falhou=1
fi

if [ "$falhou" -ne 0 ]; then
  rollback "$PREV" || true
  exit 1
fi

# --- sucesso: registra versão boa e limpa releases antigas ---
ln -sfn "$REL" "$CURRENT"
printf '%s\n' "$SHA" > "$DEPLOY_DIR/last_good_tag"

mapfile -t antigas < <(ls -1tr "$RELEASES" 2>/dev/null | grep -v "^$SHA$" || true)
excesso=$(( ${#antigas[@]} - KEEP_RELEASES + 1 ))
if [ "$excesso" -gt 0 ]; then
  for ((i = 0; i < excesso; i++)); do
    log "removendo release antiga ${antigas[$i]} (imagens mantidas para rollback)"
    rm -rf "${RELEASES:?}/${antigas[$i]}"
  done
fi

log "deploy de $SHA concluído"
