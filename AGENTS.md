# AGENTS.md — calculadora-graham

## Escopo e objetivo

- Calculadora de Graham (valor intrínseco: `V = √(22,5 × LPA × VPA)`). **Backend em Go**.
- `opencode` só trabalha no backend. Não criar/editar frontend; se houver frontend na árvore, ignore os arquivos dele.
- Fontes de dados: **brapi.dev** (cotação/LPA) + **CVM dados abertos** (PL/ações) + **B3 COTAHIST** (nome do emissor, best-effort). Autorizadas pelo usuário; não adicionar mais fontes sem pedir.

## brapi.dev (verificado em 2026-11, incl. chamadas reais)

- Autenticação: header `Authorization: Bearer $BRAPI_API_KEY`. Nunca token em query em código novo nem comitar a chave.
- Fundamentos (`modules=defaultKeyStatistics,financialData` no endpoint LEGADO `/api/quote/{ticker}`): `bookValue` (VPA) — **403 fora dos 4 fixtures com o plano atual**. O client já faz fallback automático: 403 em `modules` → refaz cotação simples (preço+LPA+logo valem p/ todos).
- LPA (`earningsPerShare`) vem na cotação simples p/ todos os tickers; `logourl` também.
- Busca: `GET /api/v2/tickers?search=...&type=stock&limit=N` — público; `page`+`limit` paginam a lista completa (usada no ETL de sugestões).
- Erros: 401 = chave; 403 = fora do plano (fallback cobre); 429 = `Retry-After` + 1 retry GET; 404 = sem `results`.
- Chave no `.env` (gitignored); `LoadDotenv(".env")` roda no boot antes de `config.Load()`; env exportada tem prioridade.

## CVM (fonte do PL/ações — layout verificado com zips reais)

- Zips: `https://dados.cvm.gov.br/dados/CIA_ABERTA/DOC/{DFP,ITR}/DADOS/{dfp,itr}_cia_aberta_{ANO}.zip` (~13MB/ano, atualização **semanal**, ODbL). Download programático passa pelo Cloudflare.
- Arquivos internos: separador `;`, encoding **latin-1** (usar `charmap.ISO8859_1` para decodificar).
- PL: `BPP_con`, `CD_CONTA == "2.03"` ("Patrimônio Líquido Consolidado"), `ORDEM_EXERC == "ÚLTIMO"`, `ESCALA_MOEDA == "MIL"` → **×1000**. Maiores `DT_REFER`, empate por maior `VERSAO`.
- Ações: `composicao_capital` → `QT_ACAO_TOTAL_CAP_INTEGR` e `QT_ACAO_TOTAL_TESOURO` (mesma escolha maior `DT_REFER`/`VERSAO`). **DENOM_CIA** vem nessa mesma planilha (evidência nominal).
- CVM identifica por **CNPJ**, não ticker — mapeamento via `internal/mapping`.
- **Escala das ações (28/… 2026): `composicao_capital` NÃO tem coluna de unidade e ~38 CNPJs (2025: 669 = 38; inclui ITSA4 e VALE3) reportam a contagem em MILHARES.** Evidência: FRE `fre_cia_aberta_capital_social_*.csv` (`Tipo_Capital=="Capital Emitido"`) traz o número cru (ITSA4 10.844.556.902; VALE3 4.539.007.580) — usado como referência de diagnóstico, não no pipeline. O pipeline compara `marketCap/preço` contra ações ×{1,1000} (`mapping.MelhorFator`), confirma com nome+numérica e persiste `fundamentos.fator_acoes` (1|1000); o handler multiplica `(total−tesouraria)×fator`. Sem isso: ITSA4 → 422 (diff ~100.000%) e VPA 1000× errado se promovido. `-promote` calibra o fator via brapi (`etl.CalibraFator`).
- **Slider de VPA (decisão do usuário): PL controlador puro (2.03 as-is)** — diverge do brapi/fundamentus que inclui não controladores (PETR4: CVM ≈ 28,5 vs brapi 37,3 — diferença esperada, não bug).
- Runtime: o que manda é a linha de maior `dt_refer` no PG entre DFP e ITR (ITR trimestral vence o balanço anual).

## Mapeamento ticker→CNPJ (regra fixada pelo usuário)

- **Confirmação automática exige duas evidências**: nome (B3 `NOMRES` do COTAHIST, ou `longName` brapi ↔ `DENOM_CIA`) **+** numérica (`marketCap÷preço` ≈ ações CVM ±10%).
- Numérica sozinha = **sugestão** (`mapping_sugestoes`), nunca serve produção. Treinar no `golang.org/x/text/unicode/norm`.
- Overrides manuais: `cmd/sync -promote TICKER=CNPJ` (upsert confirmado, fonte "manual").
- B3 COTAHIST: `bvmf.bmfbovesp.com.br/InstDados/SerHist/COTAHIST_D{ddmmaa}.zip` — registro 01: CODNEG pos. 13–24, TPMERC pos. 25–27 ("010" vista; "020" fracionário), NOMRES pos. 28–39, 245 bytes/linha, **NÃO tem CNPJ**. Host da B3 fica em grande parte das redes — código trato como "quando disponível", falha silenciosa.
- Empresas ON+PN (PETR3/PETR4) confirmam no **mesmo CNPJ** — correto: VPA usa ações totais.

## Estrutura do repo

```
/                     raiz: docker-compose.yml, FRONTEND.md (doc do front), .gitignore
/backend              TODO o código Go: cmd/{api,sync}, internal/..., go.mod, Dockerfile, .env (gitignored)
/backend/data         cache de zips CVM local (gitignored) — `go run` usa `data` relativo
/frontend             front (feito pelo usuário; NÃO editar; compose já tem serviço `front`)
```

- O front consome o contrato de FRONTEND.md. NÃO criar/editar front.

## Comandos (working)

```
go vet ./...                          # de dentro de backend/ — antes de considerar pronto
go test ./...                         # de dentro de backend/ — mocks httptest; sem rede/chave/PG no unit
go build -o /tmp/opencode/sync ./cmd/sync
docker compose up -d --build          # da RAIZ — sobe db (:5433) + backend (:8090)
docker compose exec backend /bin/calc-sync      # rodar ETL no container
docker compose exec backend /bin/calc-sync -promote PETR4=33000167000101 -listar -sem-cvm
go run ./cmd/api                      # de backend/ — .env de backend/ é lido no boot
```

- Portas do HOST: PG **5433** (5432 já em uso), backend **8090** (8080 já em uso). Dentro do compose é db:5432 e 8080.
- `cmd/sync` sem flags: CVM → B3 → sugestões/confirmados → listar. `-sem-cvm` para só promote/listar.
- Rotas: `GET /api/ativos?search=X&limit=20`, `GET /api/graham/{ticker}`, `GET /healthz`. Erro uniforme `{"erro", "ticker"}`.
- API: a propriedade de timeouts/sync no boot é do `cmd/api/main.go` (checa `SincronizadoRecentemente`, depois roda ETL sob demanda e semanal).

## Estrutura do backend (pacotes)

- `cmd/api` (entrypoint), `cmd/sync` (ETL/override), `internal/`: `config`, `brapi` (client, bearer, retry, fallback 403→simpla), `graham` (cálculo puro), `api` (chi), `store` (pgx/pgxpool, migrations `CREATE IF NOT EXISTS` no boot), `cvm` (downloader+parser), `b3` (COTAHIST), `mapping` (avaliação nome+numérica), `etl` (pipeline).
- Deps: `chi/v5`, `jackc/pgx/v5`, `golang.org/x/text` (latin-1 + NFD). Nada de utilidades desnecessárias; stdlib onde possível.
- PG de dev: docker-compose (graham/graham@localhost:5433). Sem PG, API degrada: só brapi (fixtures via modules; sem VPA para o resto → 422 com mensagem).
- Cache CVM: `config.CVMCacheDir()` lê `CVM_CACHE_DIR` (default `data` relativo ao cwd). No compose é `/app/data` (volume nomeado `cvmdata`, dono `app` — bind do host quebra por permissão). O container roda com `USER app` e `WORKDIR /app`.
- Boot do `cmd/api`: `run(ctx, db, !SincronizadoRecentemente)` (sync só se o banco está sem dados recentes) e ticker semanal roda com `true`. Já houve inversão desse flag (boot sempre sincronizava e o semanal nunca) — não reintroduzir.
- Tests: CVM parser com fixtures latin-1 zipados (`criaZipLatino`); handler via interface `FonteCVM` (fake `dbFixa`); brapi 403-fallback testado. **Não** depender de CVM real no unit (com E2E manual via `cmd/sync`).

## CI/CD (GitHub Actions)

- Workflow `.github/workflows/ci-cd.yml`: PR → `go build/vet/test` + `npm run build` (ubuntu-latest); push `main`/manual → mesmos checks e deploy no runner self-hosted (labels via var `RUNNER_LABELS`, default `self-hosted,linux`).
- Deploy `.github/scripts/deploy.sh`: `git archive HEAD` → `$DEPLOY_DIR/releases/<sha>`, gera `backend/.env` dos secrets, `docker compose build` + `up -d --no-build --wait`, healthcheck `:8090/healthz` + `:5174`, rollback para `last_good_tag`. Não usa registry/SSH.
- Invariantes: `COMPOSE_PROJECT_NAME` fixo em `calculadora-graham` (senão cria volumes novos e "perde" o banco); `image:` das services usa `${IMAGE_TAG:-latest}` (deploy usa o SHA); nunca `down -v`; não podar imagens usadas no rollback. `.env` nunca entra no `git archive` porque não é rastreado.
- Schema: migrations são `CREATE/ALTER ... IF NOT EXISTS` no boot — rollback de imagem funciona com schema mais novo; não renomeie/remova colunas sem plano.
- Secrets: `BRAPI_API_KEY` (obrigatório), `POSTGRES_PASSWORD` (opcional, default graham — em volume existente trocar exige `ALTER USER`), `BRAPI_URL`. Vars: `RUNNER_LABELS`, `DEPLOY_DIR`, `APP_URL`.

## Estado do repo

- Sem git init, sem CI, sem front. `backend/data/` e `backend/.env` são gitignored.
- PG 17 em docker-compose com tabelas `ticker_mapping`, `mapping_sugestoes`, `fundamentos` — migrations embutidas no `store.Open`.
- Decisões fechadas (plan mode 2026-11): chi; payload completo com margem/classificação; `type=stock`; **PL controlador puro**; mapping (B3→PG→manual→numérico só sugere); DFP+ITR; PG via compose; runtime só usa mapeamentos confirmados.
