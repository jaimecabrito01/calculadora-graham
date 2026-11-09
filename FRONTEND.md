# FRONTEND.md — Como consumir o backend calculadora-graham

Guia para quem desenvolve o front. Nenhum detalhe de implementação do backend é necessário: só os endpoints HTTP abaixo.

## Base

| | |
|---|---|
| **URL base (dev)** | `http://localhost:8090` |
| **Subir tudo** | `docker compose up -d` (na raiz do repo: db + backend) |
| **Formato** | JSON (`application/json; charset=utf-8`) |
| **Métodos** | Só `GET` (e `OPTIONS` para CORS) |
| **CORS** | liberado (`*`) em dev — funciona a partir de qualquer origem |

Todos os valores monetários vêm como `number` em **reais** (BRL). Percentuais também como `number` (ex.: `-73.03` = −73,03%).

---

## 1. Health check

```
GET /healthz
```

```json
{"status": "ok"}
```

Use para exibir "conectado / offline" no front.

---

## 2. Buscar ativos (autocomplete)

```
GET /api/ativos?search={termo}&limit={n}
```

| Parâmetro | Obrigatório | Default | Descrição |
|---|---|---|---|
| `search` | não | — | Parte do ticker ou nome (ex.: `petr`, `magaz`) |
| `limit` | não | `20` | 1–100 |

**Resposta 200:**

```json
{
  "ativos": [
    {
      "ticker": "WEGE3",
      "nome": "WEG S.A.",
      "long_name": "WEG SA",
      "logo_url": "https://icons.brapi.dev/icons/WEGE3.svg"
    }
  ],
  "total": 8,
  "tem_proxima": true
}
```

- Retorna **apenas ações** da B3 (FIIs/BDRs/ETFs não estão incluídos — Graham não se aplica a eles).
- `ticker` é o que você passa para o endpoint de cálculo.
- `nome` para exibir na lista; `long_name` e `logo_url` são opcionais (podem não existir).
- Para lista "mostrar tudo", chame sem `search`.

---

## 3. Calcular Graham

```
GET /api/graham/{ticker}
```

`ticker`: código da B3. O backend aceita minúsculas e espaços nas bordas, mas o ideal é enviar como vêm do autocomplete (ex.: `WEGE3`).

**Resposta 200:**

```json
{
  "ticker": "WEGE3",
  "nome": "WEG SA",
  "logo": "https://icons.brapi.dev/icons/WEGE3.svg",
  "preco_atual": 48.52,
  "lpa": 1.4940624,
  "vpa": 5.092471471335513,
  "preco_justo": 13.083981746352638,
  "margem": -73.03383811551393,
  "classificacao": "sobreavaliada",
  "vpa_fonte": "cvm",
  "vpa_referencia": "2026-09-30"
}
```

### Semântica dos campos

| Campo | Significado |
|---|---|
| `preco_atual` | Última cotação na B3 |
| `lpa` | Lucro por ação (T12M) |
| `vpa` | Valor patrimonial por ação = PL ÷ ações (já descontadas as de tesouraria) |
| `preco_justo` | **Valor de Graham** = `√(22,5 × LPA × VPA)` |
| `margem` | % = `(preco_justo − preco_atual) ÷ preco_atual × 100`. Positiva = ação negociada abaixo do preço justo |
| `classificacao` | `"descontada"` (margem ≥ 0, candidato a compra por Graham) ou `"sobreavaliada"` |
| `logo` | opcional; URL do ícone do ativo |
| `vpa_fonte` | `"brapi"` (guia planilha pronto) ou `"cvm"` (backend calculou PL da CVM ÷ ações) |
| `vpa_referencia` | opcional (só quando `vpa_fonte=cvm`); data-fim do balanço usado — ex. `"2026-09-30"` é o 3T/2026 |

> **Nota de negócio:** o VPA via CVM usa só o patrimônio **dos controladores** (decisão do produto). Pode divergir de sites como fundamentus (~24% em casos com ações de minoritários, ex.: PETR4) — não é bug.

---

## 4. Erros — contrato único

Todo erro é `{"erro": "<mensagem>", "ticker": "<ticker>"}` (ticker pode faltar em erros genéricos).

| Status | Significado | O que o front deve fazer |
|---|---|---|
| `400` | ticker malformado (símbolos estranhos, >10 chars) | validar antes; input sugerido: `regex: ^[A-Z0-9]{1,10}$` |
| `401` | chave da brapi ausente/inválida (config do backend) | exibir "serviço temporariamente sem fonte de dados" |
| `403` | plano da chave não inclui o dado (fundamento brapi) | transparente ao usuário: o backend normalmente já contorna; se vier, mostre a mensagem |
| `404` | ticker não existe na B3 | "ativo não encontrado" + sugestão de usar o autocomplete |
| `422` | Graham não aplicável: sem mapeamento CVM confirmado, LPA ≤ 0, ou dados CVM faltantes | **mensagem amigável**: "Não temos dados suficientes para calcular Graham para este ativo" (não é erro do front) |
| `429` | cota da brapi esgotada | mostrar aviso e sugerir tentar mais tarde (o backend já faz 1 retry automático) |
| `502` | falha upstream (brapi/CVM fora de alcance) | "tentar novamente" |

Exemplo:

```json
{
  "erro": "VPA indisponível: plano brapi não inclui fundamentos e não há mapeamento confirmado ticker→CNPJ ...",
  "ticker": "XYZ3"
}
```

Recomendação: trate `4xx` como "sem dados" (não é falha de rede) e `5xx`/`429` como "tentar de novo".

---

## 5. Exemplos

### curl

```bash
# autocomplete
curl "http://localhost:8090/api/ativos?search=petr&limit=5"

# cálculo
curl "http://localhost:8090/api/graham/PETR4"
```

### JavaScript (fetch)

```js
const API = 'http://localhost:8090';

// autocomplete (use debounce de ~300ms no input)
async function buscarAtivos(termo) {
  const r = await fetch(`${API}/api/ativos?search=${encodeURIComponent(termo)}&limit=10`);
  if (!r.ok) throw new Error('busca indisponível');
  const d = await r.json();
  return d.ativos; // [{ticker, nome, long_name?, logo_url?}]
}

// calcular Graham
async function calcularGraham(ticker) {
  const r = await fetch(`${API}/api/graham/${encodeURIComponent(ticker.toUpperCase())}`);
  const d = await r.json();
  if (!r.ok) {
    if (r.status === 422) throw new Error('Sem dados suficientes para este ativo.');
    if (r.status === 429) throw new Error('Limite de consultas: tente mais tarde.');
    throw new Error(d.erro ?? 'Erro ao calcular.');
  }
  return d; // {preco_justo, margem, classificacao, logo, ...}
}
```

Sugestão de UI para `margem`/`classificacao`:
- verde + sinal ▲ para `descontada`; vermelho + ▼ para `sobreavaliada`
- `preco_atual` compare visualmente com `preco_justo` (barra/mediana)

---

## 6. Comportamentos que o front deve saber

1. **Sem cache próprio necessário** — cada requisição resolve na hora; cotações mudam a cada pregão. Se quiser, cache memória por ticker por ~1 min.
2. **Dados CVM atualizam semanalmente** (download assíncrono pelo backend no boot + semanal). O `vpa_referencia` diz a data do balanço usado.
3. **Ativo sem Graham disponível ainda** pode aparecer no autocomplete e falhar no cálculo com `422` — é esperado (mapeamento em construção). O erro diz isso claramente.
4. **Timeout do servidor: 30s** por requisição; seus fetches devem abortar antes (recomendação: 25s).
5. **Cota clara**: o front não precisa de chave; quem cobra cota é o backend.

---

## 7. Rodando em outro lugar

- **Porta do backend**: defina em `backend/.env` (`PORT=...`) ou o compose já mapeia `8090:8080`.
- **Produção**: além de trocar a URL base, restrinja o CORS por origem (edite `backend/internal/api/routes.go`, middleware `cors`).
- **Sincronização manual**: `docker compose exec backend /bin/calc-sync` (etl completo) ou `-listar` para ver mapeamentos.
