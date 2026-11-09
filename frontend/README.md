# Graham — frontend

Frontend React + TypeScript + Vite para a API Go deste repositório. O resultado é o **valor justo de Graham**, não preço médio histórico de compra ou de negociação.

## Desenvolvimento

Na raiz, suba os serviços existentes:

```sh
docker compose up -d --build db backend
```

Em outro terminal:

```sh
cd frontend
npm ci
npm run dev
```

Abra http://localhost:5173. O Vite encaminha `/api` e `/healthz` para `http://localhost:8090`. Nenhuma chave brapi é usada pelo navegador.

## Docker

Na raiz:

```sh
docker compose up -d --build
```

Abra http://localhost:5174. O Nginx serve o frontend e encaminha as consultas para `backend:8080` na rede Docker.

Para usar outra porta: `FRONTEND_PORT=5175 docker compose up -d front`.

## Build

```sh
npm run build
```

Os arquivos são gerados em `dist/`. Para hospedagem fora do Compose, configure um proxy para a API ou defina `VITE_API_URL` antes do build (veja `.env.example`). `npm run preview` serve apenas os arquivos estáticos; não sobe o backend nem configura proxy para a API.

## Comportamento

- Busca por nome ou ticker, com debounce de 300 ms, cancelamento e navegação pelas setas, Enter e Escape.
- Consulta direta pelo ticker e atalhos para PETR4, VALE3, ITUB4 e WEGE3.
- Cotação, valor de Graham, LPA, VPA, margem e origem dos fundamentos conforme contrato em `../FRONTEND.md`.
- Valores ausentes não são convertidos em zero. Margem: `(valor justo − cotação) / cotação × 100`.
- Requisições com timeout de 25 segundos e tratamento de API offline, ativo inexistente, fundamentos insuficientes e limite de consultas.
- Layout responsivo e movimento de pressionar botões condicionado à preferência de movimento reduzido.

## Verificação nesta implementação

- Build de produção e checagem TypeScript aprovados (`npm run build`).
- `docker compose config --quiet` aprovado.
- Backend: `go vet ./...` e `go test ./...` aprovados.
- Cliente HTTP verificado com respostas simuladas: sucesso, campos nulos, 401/404/422/429/502, resposta inválida, falha de rede e cancelamento.
- better-ui: nenhum achado acionável na revisão estática dos estados normal, foco, hover, pressionado, desabilitado, carregamento, vazio e erro. Ícones Lucide, bordas de estrutura, sombras de elevação e movimento reduzido revisados.
- **Não verificado:** aparência e interações em navegador (Chromium bloqueado pelas permissões do ambiente), execução dos containers (socket Docker indisponível) e consultas com dados reais (API local sem responder durante a verificação).
