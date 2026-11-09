# Calculadora Graham

Calculadora de **valor intrínseco** de ações da B3 pelo método de Benjamin Graham:

```
preço justo = √(22,5 × LPA × VPA)
```

Informa também a **margem de segurança** (`(preço justo − preço atual) ÷ preço atual`) e a classificação `descontada` / `sobreavaliada`.

## Arquitetura

```
frontend (React + Vite)  →  API Go (chi)  →  brapi.dev (cotação, LPA)
                                   ↕
                              PostgreSQL
                                   ↑
                    ETL (CVM dados abertos + B3 COTAHIST)
```

- **API** (`backend/cmd/api`): serve o front e cai para fundamentos da CVM quando o plano da brapi não libera o VPA.
- **ETL** (`backend/cmd/sync`): baixa DFP/ITR da CVM, grava PL e quantidade de ações, mapeia ticker→CNPJ (evidência de nome **e** numérica) e roda no boot quando o banco está vazio e semanalmente.
- **PostgreSQL**: tabelas `fundamentos`, `ticker_mapping` e `mapping_sugestoes`.

## Stack

Go 1.27 (chi, pgx) · PostgreSQL 17 · React 19 + TypeScript + Vite · Docker Compose

## Rodando

```sh
docker compose up -d --build
```

| Serviço | URL no host |
|---|---|
| Frontend | http://localhost:5174 |
| API | http://localhost:8090 |
| PostgreSQL | `localhost:5433` |

Portas 8080/5432/5173 podem estar ocupadas na máquina; o compose usa 8090/5433/5174.

Configuração em `backend/.env` (não versionado):

```sh
BRAPI_API_KEY=sua_chave
PORT=8080
DATABASE_URL=postgres://graham:graham@localhost:5433/graham?sslmode=disable
```

No compose o `DATABASE_URL` é sobrescrito para `db:5432`. O cache dos zips da CVM fica no volume `cvmdata` (`/app/data`).

Sincronização manual:

```sh
docker compose exec backend /bin/calc-sync
docker compose exec backend /bin/calc-sync -listar
docker compose exec backend /bin/calc-sync -promote PETR4=33000167000101
```

## CI/CD (GitHub Actions)

`.github/workflows/ci-cd.yml`:

- **Pull request para `main`**: `go build/vet/test` (em `backend/`) e `npm run build` (em `frontend/`) em `ubuntu-latest`. Nada de deploy em PR.
- **Push em `main`**: os mesmos checks e, se passarem, deploy no **runner self-hosted** da VM.
- **Manual**: `workflow_dispatch`.

O deploy (`.github/scripts/deploy.sh`) extrai o commit para `releases/<sha>` (só arquivos rastreados), gera `backend/.env` a partir dos secrets, constrói com tag = SHA, sobe com `docker compose up -d --no-build --wait` e valida `:8090/healthz` + `:5174/`. Em falha, volta para a última release boa (`last_good_tag`). Volumes, banco e cache CVM ficam fora do checkout; o nome do projeto Compose é fixado em `calculadora-graham` para reaproveitar `pgdata`/`cvmdata`.

### Secrets (environment `deployment`)

| Secret | Obrigatório | Uso |
|---|---|---|
| `BRAPI_API_KEY` | sim | grava em `backend/.env` (chave da brapi) |
| `POSTGRES_PASSWORD` | não | default `graham`. Só vale em volume novo; trocar depois exige `ALTER USER` no banco |
| `BRAPI_URL` | não | default `https://brapi.dev/api` (só para testes) |

### Variables

| Variable | Obrigatório | Uso |
|---|---|---|
| `RUNNER_LABELS` | não | JSON, default `["self-hosted","linux"]`; ex. `["self-hosted","linux","graham"]` |
| `DEPLOY_DIR` | não | default `~/calculadora-graham` no runner |
| `APP_URL` | não | URL exibida no environment `deployment` |

### Pré-requisitos na VM

- Docker Engine + Compose v2 e o usuário do runner no grupo `docker`.
- Runner self-hosted registrado com as labels `self-hosted` e `linux` (padrão de qualquer runner Linux); para exigir uma label específica, defina `RUNNER_LABELS`.
- `DEPLOY_DIR` gravável pelo usuário do runner; portas 8090/5174/5433 livres.
- Primeiro deploy baixa as imagens base (Go, Node, Nginx, Postgres) e reconstrói tudo.

Recomendado: em Settings → Branches, exigir os checks **Backend (vet + testes)** e **Frontend (build)** em PRs para `main`.

## API

| Rota | Descrição |
|---|---|
| `GET /healthz` | health check |
| `GET /api/ativos?search=X&limit=20` | autocomplete de ações |
| `GET /api/graham/{ticker}` | cálculo do preço justo |

Contrato completo (payloads, erros, exemplos) em [FRONTEND.md](FRONTEND.md).

## Limitações conhecidas

- **Plano brapi**: fundamentos (`bookValue`) só vêm para alguns tickers; nos demais o VPA é calculado pela CVM: `PL controlador (conta 2.03) ÷ (ações − tesouraria)`.
- **Escala das ações na CVM**: o arquivo `composicao_capital` não tem coluna de unidade e parte dos emissores reporta em milhares (ITSA4, VALE3, ABEV3…). O ETL detecta e corrige via `fundamentos.fator_acoes`.
- **PL controlador puro**: por decisão de produto, não inclui participação de não controladores — pode divergir de outros sites.
- **B3 COTAHIST**: usado só como evidência de nome, em modo best-effort (pode estar inacessível).

## Estrutura

```
backend/        código Go (cmd/api, cmd/sync, internal/…), Dockerfile, .env
frontend/       React + Vite (build servido por Nginx no compose)
docker-compose.yml
FRONTEND.md     contrato HTTP para o frontend
AGENTS.md       regras e decisões de desenvolvimento
```
