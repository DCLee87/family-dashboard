import { useCallback, useEffect, useState } from "react";

type Health = {
  status: "loading" | "ok" | "error";
  database: "loading" | "ok" | "error";
};

type Runtime = {
  version: string;
  uptimeSeconds: number;
  startCount: number;
};

const initialHealth: Health = { status: "loading", database: "loading" };

export default function App() {
  const [health, setHealth] = useState<Health>(initialHealth);
  const [runtime, setRuntime] = useState<Runtime | null>(null);
  const [checkedAt, setCheckedAt] = useState<Date | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [healthResponse, runtimeResponse] = await Promise.all([
        fetch("/api/health", { cache: "no-store" }),
        fetch("/api/runtime", { cache: "no-store" }),
      ]);
      if (!healthResponse.ok || !runtimeResponse.ok) throw new Error("unavailable");
      setHealth(await healthResponse.json());
      setRuntime(await runtimeResponse.json());
    } catch {
      setHealth({ status: "error", database: "error" });
      setRuntime(null);
    } finally {
      setCheckedAt(new Date());
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const healthy = health.status === "ok" && health.database === "ok";

  return (
    <main>
      <header>
        <p className="eyebrow">E1 · MINIMAL RUNTIME</p>
        <h1>우리 가족 대시보드</h1>
        <p className="subtitle">NAS에서 가볍고 오래 실행될 준비가 되었는지 확인하는 첫 화면입니다.</p>
      </header>

      <section className={`status-card ${healthy ? "healthy" : "unhealthy"}`}>
        <div>
          <span className="status-dot" aria-hidden="true" />
          <p className="label">서비스 상태</p>
          <strong>{health.status === "loading" ? "확인 중" : healthy ? "정상" : "연결 필요"}</strong>
        </div>
        <button type="button" onClick={() => void refresh()}>지금 확인</button>
      </section>

      <section className="metrics" aria-label="런타임 정보">
        <article>
          <p className="label">데이터베이스</p>
          <strong>{health.database === "ok" ? "SQLite 정상" : health.database === "loading" ? "확인 중" : "점검 필요"}</strong>
        </article>
        <article>
          <p className="label">재시작 횟수</p>
          <strong>{runtime?.startCount ?? "—"}</strong>
        </article>
        <article>
          <p className="label">가동 시간</p>
          <strong>{runtime ? formatDuration(runtime.uptimeSeconds) : "—"}</strong>
        </article>
        <article>
          <p className="label">버전</p>
          <strong>{runtime?.version ?? "—"}</strong>
        </article>
      </section>

      <footer>
        마지막 확인 {checkedAt ? checkedAt.toLocaleTimeString("ko-KR") : "대기 중"} · 30초마다 자동 갱신
      </footer>
    </main>
  );
}

function formatDuration(totalSeconds: number) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  return hours > 0 ? `${hours}시간 ${minutes}분` : `${minutes}분 ${seconds}초`;
}
