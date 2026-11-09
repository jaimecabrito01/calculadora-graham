import { useEffect, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  ArrowUpRight,
  BookOpen,
  ChartNoAxesCombined,
  Check,
  ChevronRight,
  CircleAlert,
  Database,
  Info,
  RefreshCw,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import { ApiError, request, type Ativo, type Resultado } from "./api";

const money = (value: number | null) =>
  value == null
    ? "Indisponível"
    : new Intl.NumberFormat("pt-BR", {
        style: "currency",
        currency: "BRL",
      }).format(value);
const percent = (value: number) =>
  `${value > 0 ? "+" : ""}${value.toLocaleString("pt-BR", { maximumFractionDigits: 2 })}%`;
const popular = ["PETR4", "VALE3", "ITUB4", "WEGE3"];

function AssetLogo({ ticker, src }: { ticker: string; src?: string }) {
  const [failed, setFailed] = useState(false);
  return (
    <span className="asset-logo">
      {src && !failed ? (
        <img src={src} alt="" onError={() => setFailed(true)} />
      ) : (
        ticker.slice(0, 2)
      )}
    </span>
  );
}

function Result({ data }: { data: Resultado }) {
  const discounted = data.classificacao === "descontada";
  const comparable =
    data.preco_atual != null &&
    data.preco_justo != null &&
    data.preco_atual > 0 &&
    data.preco_justo > 0;
  const max = Math.max(data.preco_atual ?? 0, data.preco_justo ?? 0, 1);
  return (
    <section className="result" aria-labelledby="result-title">
      <div className="result-heading">
        <div className="asset-heading">
          <AssetLogo key={data.ticker} ticker={data.ticker} src={data.logo} />
          <div>
            <h2 id="result-title">
              {data.ticker} <span className="exchange">B3</span>
            </h2>
            <p>{data.nome}</p>
          </div>
        </div>
        <span className="result-label">
          <Check size={14} /> Consulta concluída
        </span>
      </div>
      <div className="value-grid">
        <div className="fair-value">
          <span className="eyebrow">VALOR JUSTO DE GRAHAM</span>
          <strong>{money(data.preco_justo)}</strong>
          <span>Valor intrínseco estimado por ação</span>
        </div>
        <div className="current-value">
          <span>Cotação atual</span>
          <strong>{money(data.preco_atual)}</strong>
          {data.classificacao && (
            <span className={`badge ${discounted ? "positive" : "negative"}`}>
              {discounted ? (
                <ArrowDown size={14} />
              ) : (
                <ArrowUpRight size={14} />
              )}
              {discounted ? "Abaixo do valor justo" : "Acima do valor justo"}
            </span>
          )}
        </div>
      </div>
      {comparable && (
        <div className="comparison">
          <div className="bar-row">
            <span>Cotação atual</span>
            <div className="bar-track">
              <div
                className="bar current-bar"
                style={{ width: `${(data.preco_atual! / max) * 100}%` }}
              />
            </div>
            <b>{money(data.preco_atual)}</b>
          </div>
          <div className="bar-row">
            <span>Valor de Graham</span>
            <div className="bar-track">
              <div
                className="bar fair-bar"
                style={{ width: `${(data.preco_justo! / max) * 100}%` }}
              />
            </div>
            <b>{money(data.preco_justo)}</b>
          </div>
        </div>
      )}
      <div className="fundamentals">
        <div>
          <span>
            Lucro por ação{" "}
            <abbr title="Lucro por ação nos últimos 12 meses">LPA</abbr>
          </span>
          <strong>{money(data.lpa)}</strong>
        </div>
        <div>
          <span>
            Valor patrimonial{" "}
            <abbr title="Valor patrimonial por ação">VPA</abbr>
          </span>
          <strong>{money(data.vpa)}</strong>
        </div>
        <div>
          <span>Margem sobre a cotação</span>
          <strong
            className={
              data.margem == null
                ? ""
                : discounted
                  ? "positive-text"
                  : "negative-text"
            }
          >
            {data.margem == null ? "Indisponível" : percent(data.margem)}
          </strong>
        </div>
      </div>
      <div className="source">
        <Database size={14} />
        <span>
          Cotação e LPA: brapi
          {data.vpa_fonte
            ? ` · VPA: ${data.vpa_fonte === "cvm" ? "CVM" : "brapi"}`
            : ""}
          {data.vpa_referencia
            ? ` · Balanço de ${new Date(`${data.vpa_referencia}T12:00:00`).toLocaleDateString("pt-BR")}`
            : ""}
        </span>
      </div>
      {data.vpa_fonte === "cvm" && (
        <p className="source-note">
          O VPA considera o patrimônio dos controladores e pode divergir de
          outras plataformas.
        </p>
      )}
    </section>
  );
}

export default function App() {
  const [query, setQuery] = useState("");
  const [suggestions, setSuggestions] = useState<Ativo[]>([]);
  const [open, setOpen] = useState(false);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [active, setActive] = useState(-1);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState("");
  const [result, setResult] = useState<Resultado | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [connection, setConnection] = useState("Verificando conexão");
  const controller = useRef<AbortController | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const wrapper = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const abort = new AbortController();
    request<{ status: string }>("/healthz", abort.signal)
      .then(() => setConnection("API conectada"))
      .catch(() => {
        if (!abort.signal.aborted) setConnection("API indisponível");
      });
    return () => {
      abort.abort();
      controller.current?.abort();
    };
  }, []);

  useEffect(() => {
    const abort = new AbortController();
    setSuggestions([]);
    setActive(-1);
    setSearchError("");
    if (!query.trim() || !open) {
      setSearching(false);
      return;
    }
    setSearching(true);
    const timer = setTimeout(() => {
      request<{ ativos: Ativo[] }>(
        `/api/ativos?search=${encodeURIComponent(query.trim())}&limit=6`,
        abort.signal,
      )
        .then((data) => setSuggestions(data.ativos ?? []))
        .catch(() => {
          if (!abort.signal.aborted)
            setSearchError(
              "Busca indisponível. Você ainda pode consultar pelo código do ativo.",
            );
        })
        .finally(() => {
          if (!abort.signal.aborted) setSearching(false);
        });
    }, 300);
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  }, [query, open]);

  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!wrapper.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);

  async function calculate(ticker: string) {
    const normalized = ticker.trim().toUpperCase();
    setOpen(false);
    if (!/^[A-Z0-9]{1,10}$/.test(normalized)) {
      setError(
        new ApiError(
          400,
          "Escolha um ativo na busca ou digite um código válido, como PETR4.",
        ),
      );
      return;
    }
    controller.current?.abort();
    const abort = new AbortController();
    controller.current = abort;
    setQuery(normalized);
    setSelected(normalized);
    setLoading(true);
    setError(null);
    setResult(null);
    try {
      const data = await request<Resultado>(
        `/api/graham/${encodeURIComponent(normalized)}`,
        abort.signal,
      );
      if (!abort.signal.aborted) {
        setResult(data);
        setConnection("API conectada");
      }
    } catch (err) {
      if (!abort.signal.aborted) {
        setError(
          err instanceof ApiError
            ? err
            : new ApiError(
                0,
                "Não foi possível ler o resultado. Tente novamente.",
              ),
        );
        if (err instanceof ApiError && err.status === 0)
          setConnection("API indisponível");
      }
    } finally {
      if (!abort.signal.aborted) setLoading(false);
    }
  }

  return (
    <>
      <header className="header">
        <a className="brand" href="./" aria-label="Graham, início">
          <span className="brand-symbol">
            <ChartNoAxesCombined size={23} strokeWidth={2} />
          </span>
          graham<span className="brand-dot">.</span>
        </a>
        <nav aria-label="Navegação principal">
          <a href="#calculadora" className="nav-active">
            Calculadora
          </a>
          <a href="#metodo">
            O método <ArrowUpRight size={14} />
          </a>
        </nav>
        <span
          className={`connection ${connection === "API conectada" ? "connected" : ""}`}
        >
          <i />
          {connection}
        </span>
      </header>
      <main id="calculadora">
        <section className="intro">
          <span className="eyebrow">
            <span className="small-line" /> INVESTIR COMEÇA POR ENTENDER
          </span>
          <h1>
            Preço é o que você paga.
            <br />
            <span>Valor é o que você encontra.</span>
          </h1>
          <p>
            Descubra o valor justo de uma ação com a fórmula de Benjamin Graham.
            <br className="desktop-break" /> Menos ruído. Mais fundamentos para
            a sua análise.
          </p>
        </section>
        <div className="workspace">
          <div className="calculator-column">
            <section className="search-card" aria-labelledby="search-title">
              <div className="section-title">
                <span className="step-number">01</span>
                <h2 id="search-title">Qual ação vamos analisar?</h2>
                <span className="market-label">AÇÕES BRASILEIRAS</span>
              </div>
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  void calculate(
                    open && active >= 0 && suggestions[active]
                      ? suggestions[active].ticker
                      : query,
                  );
                }}
              >
                <label className="input-label" htmlFor="asset-search">
                  Nome da empresa ou código do ativo
                </label>
                <div className="search-wrapper" ref={wrapper}>
                  <div className="search-field">
                    <Search size={20} />
                    <input
                      id="asset-search"
                      ref={input}
                      value={query}
                      placeholder="Busque por PETR4, Vale, Itaú…"
                      autoComplete="off"
                      role="combobox"
                      aria-autocomplete="list"
                      aria-expanded={open && !!query.trim()}
                      aria-controls="asset-options"
                      aria-activedescendant={
                        open && active >= 0 ? `asset-${active}` : undefined
                      }
                      onFocus={() => setOpen(true)}
                      onChange={(event) => {
                        setQuery(event.target.value);
                        setOpen(true);
                      }}
                      onKeyDown={(event) => {
                        if (event.key === "Escape") {
                          setOpen(false);
                          setActive(-1);
                        }
                        if (
                          event.key === "ArrowDown" ||
                          event.key === "ArrowUp"
                        ) {
                          event.preventDefault();
                          setOpen(true);
                          setActive((index) =>
                            event.key === "ArrowDown"
                              ? Math.min(index + 1, suggestions.length - 1)
                              : Math.max(index - 1, -1),
                          );
                        }
                      }}
                    />
                    {query && (
                      <button
                        className="clear-button"
                        type="button"
                        aria-label="Limpar busca"
                        onClick={() => {
                          setQuery("");
                          setOpen(false);
                          input.current?.focus();
                        }}
                      >
                        <X size={16} />
                      </button>
                    )}
                    <button
                      className="calculate-button"
                      disabled={!query.trim() || loading}
                      type="submit"
                    >
                      {loading ? "Calculando…" : "Analisar ação"}
                      <ArrowRight size={17} />
                    </button>
                  </div>
                  {open && !!query.trim() && (
                    <div className="suggestions">
                      <ul
                        id="asset-options"
                        role="listbox"
                        aria-label="Ativos encontrados"
                      >
                        {suggestions.map((asset, index) => (
                          <li
                            id={`asset-${index}`}
                            role="option"
                            aria-selected={active === index}
                            key={asset.ticker}
                            className={active === index ? "option-active" : ""}
                            onPointerDown={(event) => event.preventDefault()}
                            onClick={() => void calculate(asset.ticker)}
                          >
                            <AssetLogo
                              ticker={asset.ticker}
                              src={asset.logo_url}
                            />
                            <span>
                              <b>{asset.ticker}</b>
                              <small>{asset.nome}</small>
                            </span>
                            <ChevronRight size={17} />
                          </li>
                        ))}
                      </ul>
                      {searching && <p role="status">Buscando ações…</p>}
                      {searchError && <p role="status">{searchError}</p>}
                      {!searching && !searchError && !suggestions.length && (
                        <p role="status">
                          Nenhuma sugestão. Pressione Enter para consultar o
                          código.
                        </p>
                      )}
                    </div>
                  )}
                </div>
              </form>
              <div className="popular">
                <span>Explore também</span>
                {popular.map((ticker) => (
                  <button
                    key={ticker}
                    type="button"
                    disabled={loading}
                    onClick={() => void calculate(ticker)}
                  >
                    {ticker}
                    <ArrowUpRight size={12} />
                  </button>
                ))}
              </div>
            </section>
            <div
              className="result-region"
              aria-live="polite"
              aria-busy={loading}
            >
              {loading ? (
                <section className="empty-state loading-state">
                  <RefreshCw size={28} />
                  <h2>Analisando {selected}</h2>
                  <p>
                    Consultando cotação e fundamentos para calcular o valor
                    justo.
                  </p>
                  <div className="loading-line" />
                </section>
              ) : error ? (
                <section className="error-state" role="alert">
                  <span className="error-icon">
                    <CircleAlert size={24} />
                  </span>
                  <h2>
                    {error.status === 422
                      ? "Este ativo precisa de mais dados"
                      : "Não foi possível concluir a análise"}
                  </h2>
                  <p>{error.message}</p>
                  {selected && error.status !== 400 && (
                    <button
                      className="secondary-button"
                      onClick={() => void calculate(selected)}
                    >
                      <RefreshCw size={16} />
                      Tentar novamente
                    </button>
                  )}
                </section>
              ) : result ? (
                <Result data={result} />
              ) : (
                <section className="empty-state">
                  <div className="empty-illustration">
                    <div className="mini-bars">
                      <i />
                      <i />
                      <i />
                      <i />
                    </div>
                    <span>
                      <Search size={20} />
                    </span>
                  </div>
                  <h2>Uma boa análise começa aqui.</h2>
                  <p>
                    Busque uma ação para descobrir seu valor de Graham,
                    <br className="desktop-break" /> comparar com a cotação e
                    conhecer os fundamentos.
                  </p>
                  <span className="empty-caption">
                    <ShieldCheck size={14} /> Dados reais. Cálculo transparente.
                  </span>
                </section>
              )}
            </div>
            <div className="disclaimer">
              <Info size={16} />
              <p>
                Uma ferramenta para apoiar sua análise. O valor de Graham é uma
                estimativa e não representa recomendação de compra ou venda.
              </p>
            </div>
          </div>
          <aside className="method-card" id="metodo">
            <div className="method-top">
              <BookOpen size={20} />
              <span>POR TRÁS DO NÚMERO</span>
            </div>
            <h2>
              Um clássico.
              <br />
              Ainda relevante.
            </h2>
            <p>
              Benjamin Graham propôs uma forma de estimar o valor de uma ação a
              partir de dois fundamentos.
            </p>
            <div
              className="formula"
              aria-label="Valor igual à raiz quadrada de 22,5 vezes LPA vezes VPA"
            >
              <span>VALOR DE GRAHAM</span>
              <div>
                √<span className="radicand">22,5 × LPA × VPA</span>
              </div>
            </div>
            <dl>
              <div>
                <dt>
                  <span>01</span> Lucro por ação <b>LPA</b>
                </dt>
                <dd>
                  Quanto a empresa lucra para cada ação, nos últimos 12 meses.
                </dd>
              </div>
              <div>
                <dt>
                  <span>02</span> Valor patrimonial <b>VPA</b>
                </dt>
                <dd>
                  O patrimônio líquido da empresa dividido pelo número de ações.
                </dd>
              </div>
              <div>
                <dt>
                  <span>×</span> A constante <b>22,5</b>
                </dt>
                <dd>
                  Combina um P/L de 15 com um P/VP de 1,5, referências do
                  método.
                </dd>
              </div>
            </dl>
            <div className="method-note">
              <ShieldCheck size={18} />
              <p>
                O método exige LPA e VPA positivos. Use outros indicadores para
                complementar sua decisão.
              </p>
            </div>
          </aside>
        </div>
        <section
          className="principles"
          aria-label="Características da ferramenta"
        >
          <div>
            <Database size={19} />
            <span>
              <b>Fundamentos de verdade</b>
              <small>Dados da brapi e da CVM</small>
            </span>
          </div>
          <div>
            <ChartNoAxesCombined size={19} />
            <span>
              <b>Do preço ao valor</b>
              <small>Cotação e valor justo, lado a lado</small>
            </span>
          </div>
          <div>
            <ShieldCheck size={19} />
            <span>
              <b>Sem caixa-preta</b>
              <small>Você conhece cada parte do cálculo</small>
            </span>
          </div>
        </section>
      </main>
      <footer>
        <span className="footer-brand">graham.</span>
        <p>Clareza para investir com consciência.</p>
        <span>Feito para quem olha além da cotação.</span>
      </footer>
    </>
  );
}
