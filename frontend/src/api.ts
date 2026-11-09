export interface Ativo {
  ticker: string;
  nome: string;
  long_name?: string;
  logo_url?: string;
}
export interface Resultado {
  ticker: string;
  nome: string;
  logo?: string;
  preco_atual: number | null;
  lpa: number | null;
  vpa: number | null;
  preco_justo: number | null;
  margem: number | null;
  classificacao: string;
  vpa_fonte?: string;
  vpa_referencia?: string;
}
const base = (import.meta.env.VITE_API_URL ?? "").replace(/\/$/, "");
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export async function request<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${base}${path}`, {
      signal: signal
        ? AbortSignal.any([signal, AbortSignal.timeout(25000)])
        : AbortSignal.timeout(25000),
    });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new ApiError(
      0,
      "Não foi possível conectar ao servidor. Verifique se o backend está em execução e tente novamente.",
    );
  }
  const data = await response.json().catch(() => {
    if (response.ok) {
      throw new ApiError(
        502,
        "O servidor retornou uma resposta inválida. Tente novamente.",
      );
    }
    return {};
  });
  if (!response.ok) {
    const messages: Record<number, string> = {
      401: "A fonte de dados está temporariamente indisponível. Tente novamente mais tarde.",
      404: "Ativo não encontrado. Confira o código ou escolha uma sugestão da busca.",
      422: "Não temos fundamentos suficientes para calcular Graham para este ativo. Isso pode ocorrer com lucro negativo ou dados patrimoniais indisponíveis.",
      429: "O limite de consultas foi atingido. Aguarde um pouco antes de tentar novamente.",
      502: "A fonte de dados não respondeu. Tente novamente em alguns instantes.",
    };
    throw new ApiError(
      response.status,
      messages[response.status] ??
        data.erro ??
        "Não foi possível concluir a consulta.",
    );
  }
  return data as T;
}
