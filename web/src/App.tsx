import { FormEvent, useCallback, useEffect, useState } from "react";

type Health = {
  status: "loading" | "ok" | "error";
  database: "loading" | "ok" | "error";
};

type Runtime = {
  version: string;
  uptimeSeconds: number;
  startCount: number;
};

type SetupResult = {
  recoveryCode: string;
  device: {
    name: string;
  };
};

type DeviceAuth = {
  status: "authenticated";
  device: {
    name: string;
    type: string;
  };
  permissions: {
    view: boolean;
    admin: boolean;
    canUnlockAdmin: boolean;
  };
};

const initialHealth: Health = { status: "loading", database: "loading" };

export default function App() {
  const [health, setHealth] = useState<Health>(initialHealth);
  const [runtime, setRuntime] = useState<Runtime | null>(null);
  const [checkedAt, setCheckedAt] = useState<Date | null>(null);
  const [setupRequired, setSetupRequired] = useState<boolean | null>(null);
  const [deviceAuth, setDeviceAuth] = useState<DeviceAuth | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [healthResponse, runtimeResponse, setupResponse, deviceResponse] = await Promise.all([
        fetch("/api/health", { cache: "no-store" }),
        fetch("/api/runtime", { cache: "no-store" }),
        fetch("/api/setup/status", { cache: "no-store" }),
        fetch("/api/auth/device", { cache: "no-store" }),
      ]);
      if (!healthResponse.ok || !runtimeResponse.ok || !setupResponse.ok) {
        throw new Error("unavailable");
      }
      setHealth(await healthResponse.json());
      setRuntime(await runtimeResponse.json());
      const setup = await setupResponse.json() as { setupRequired: boolean };
      setSetupRequired(setup.setupRequired);
      setDeviceAuth(deviceResponse.ok ? await deviceResponse.json() : null);
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

  if (setupRequired) {
    return <SetupScreen onComplete={() => setSetupRequired(false)} />;
  }

  const healthy = health.status === "ok" && health.database === "ok";

  return (
    <main>
      <header>
        <p className="eyebrow">E2 · PRIVATE FAMILY RUNTIME</p>
        <h1>우리 가족 대시보드</h1>
        <p className="subtitle">가족 기기만 안전하게 연결되는 NAS 대시보드의 현재 상태입니다.</p>
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

      <AdminControl auth={deviceAuth} onChanged={refresh} />

      <footer>
        마지막 확인 {checkedAt ? checkedAt.toLocaleTimeString("ko-KR") : "대기 중"} · 30초마다 자동 갱신
      </footer>
    </main>
  );
}

function AdminControl({
  auth,
  onChanged,
}: {
  auth: DeviceAuth | null;
  onChanged: () => Promise<void>;
}) {
  const [showUnlock, setShowUnlock] = useState(false);
  const [pin, setPin] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (!auth) {
    return (
      <section className="admin-card">
        <div>
          <p className="label">기기 인증</p>
          <strong>등록되지 않은 브라우저</strong>
          <p>이 기기에서는 가족 정보와 관리자 기능을 사용할 수 없습니다.</p>
        </div>
      </section>
    );
  }

  async function unlock(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const response = await fetch("/api/admin/unlock", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pin }),
      });
      if (!response.ok) {
        if (response.status === 429) {
          throw new Error("PIN 입력이 5회 실패해 5분 동안 잠겼습니다.");
        }
        if (response.status === 401) {
          throw new Error("PIN이 올바르지 않습니다.");
        }
        throw new Error("관리자 모드를 열지 못했습니다.");
      }
      setPin("");
      setShowUnlock(false);
      await onChanged();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "관리자 모드를 열지 못했습니다.");
    } finally {
      setSubmitting(false);
    }
  }

  async function lock() {
    setError("");
    setSubmitting(true);
    try {
      const csrf = readCookie("family_dashboard_csrf");
      const response = await fetch("/api/admin/lock", {
        method: "POST",
        headers: { "X-CSRF-Token": csrf },
      });
      if (!response.ok) throw new Error("관리자 모드를 잠그지 못했습니다.");
      await onChanged();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "관리자 모드를 잠그지 못했습니다.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className={`admin-card ${auth.permissions.admin ? "admin-active" : ""}`}>
      <div>
        <p className="label">현재 기기</p>
        <strong>{auth.device.name}</strong>
        <p>
          {auth.permissions.admin
            ? "관리자 모드가 열려 있습니다. 마지막 관리자 작업 후 10분이 지나면 자동으로 잠깁니다."
            : "보기 모드입니다. 설정을 변경할 때만 관리자 모드를 여세요."}
        </p>
      </div>
      {auth.permissions.admin ? (
        <button type="button" disabled={submitting} onClick={() => void lock()}>
          지금 잠그기
        </button>
      ) : auth.permissions.canUnlockAdmin && !showUnlock ? (
        <button type="button" onClick={() => setShowUnlock(true)}>관리자 모드 열기</button>
      ) : null}
      {showUnlock && !auth.permissions.admin && (
        <form className="unlock-form" onSubmit={unlock}>
          <label>
            <span>관리자 PIN</span>
            <input
              type="password"
              inputMode="numeric"
              autoComplete="current-password"
              maxLength={4}
              pattern="[0-9]{4}"
              value={pin}
              onChange={(event) => setPin(event.target.value)}
              autoFocus
              required
            />
          </label>
          <div className="button-row">
            <button type="submit" disabled={submitting}>
              {submitting ? "확인 중…" : "열기"}
            </button>
            <button type="button" className="secondary" onClick={() => {
              setShowUnlock(false);
              setPin("");
              setError("");
            }}>
              취소
            </button>
          </div>
        </form>
      )}
      {error && <p className="form-error" role="alert">{error}</p>}
    </section>
  );
}

function SetupScreen({ onComplete }: { onComplete: () => void }) {
  const [initialSetupCode, setInitialSetupCode] = useState("");
  const [deviceName, setDeviceName] = useState("");
  const [pin, setPin] = useState("");
  const [pinConfirmation, setPinConfirmation] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<SetupResult | null>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (!/^\d{4}$/.test(pin)) {
      setError("관리자 PIN은 숫자 4자리로 입력해주세요.");
      return;
    }
    if (pin !== pinConfirmation) {
      setError("관리자 PIN 확인 값이 일치하지 않습니다.");
      return;
    }
    setSubmitting(true);
    try {
      const response = await fetch("/api/setup/complete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ initialSetupCode, deviceName, pin }),
      });
      const body = await response.json() as SetupResult & { status?: string };
      if (!response.ok) {
        if (response.status === 403) {
          throw new Error("이 연결에서는 최초 설정이 허용되지 않습니다. NAS 네트워크 설정을 확인해주세요.");
        }
        if (response.status === 401) {
          throw new Error("초기 설정 코드가 잘못되었거나 만료되었습니다.");
        }
        throw new Error("설정을 완료하지 못했습니다. 입력값과 서버 로그를 확인해주세요.");
      }
      setInitialSetupCode("");
      setPin("");
      setPinConfirmation("");
      setResult(body);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "설정을 완료하지 못했습니다.");
    } finally {
      setSubmitting(false);
    }
  }

  if (result) {
    return (
      <main className="setup-layout">
        <section className="setup-card recovery-card">
          <p className="eyebrow">최초 설정 완료</p>
          <h1>복구 코드를 보관하세요</h1>
          <p className="subtitle">
            이 코드는 지금 한 번만 표시됩니다. 종이에 적거나 신뢰할 수 있는 비밀번호 관리자에 보관하세요.
          </p>
          <div className="recovery-code" role="status">{result.recoveryCode}</div>
          <p className="security-note">
            GitHub, 일반 메모, 메신저, NAS 데이터 폴더에는 저장하지 마세요.
          </p>
          <button type="button" onClick={onComplete}>안전하게 보관했습니다</button>
        </section>
      </main>
    );
  }

  return (
    <main className="setup-layout">
      <header className="setup-intro">
        <p className="eyebrow">E2 · FIRST TRUSTED DEVICE</p>
        <h1>가족 공간을 안전하게 시작합니다</h1>
        <p className="subtitle">
          NAS 로그의 일회용 코드로 이 Mac을 최초 신뢰 기기로 등록하고 관리자 PIN을 설정하세요.
        </p>
      </header>

      <form className="setup-card" onSubmit={submit}>
        <label>
          <span>초기 설정 코드</span>
          <input
            autoComplete="one-time-code"
            value={initialSetupCode}
            onChange={(event) => setInitialSetupCode(event.target.value)}
            required
          />
        </label>
        <label>
          <span>이 기기의 이름</span>
          <input
            autoComplete="off"
            maxLength={80}
            placeholder="예: 내 MacBook"
            value={deviceName}
            onChange={(event) => setDeviceName(event.target.value)}
            required
          />
        </label>
        <div className="pin-fields">
          <label>
            <span>관리자 PIN</span>
            <input
              type="password"
              inputMode="numeric"
              autoComplete="new-password"
              maxLength={4}
              pattern="[0-9]{4}"
              value={pin}
              onChange={(event) => setPin(event.target.value)}
              required
            />
          </label>
          <label>
            <span>PIN 확인</span>
            <input
              type="password"
              inputMode="numeric"
              autoComplete="new-password"
              maxLength={4}
              pattern="[0-9]{4}"
              value={pinConfirmation}
              onChange={(event) => setPinConfirmation(event.target.value)}
              required
            />
          </label>
        </div>
        <p className="security-note">
          생년월일, 전화번호 끝자리, 0000, 1234처럼 추측하기 쉬운 PIN은 피하세요.
        </p>
        {error && <p className="form-error" role="alert">{error}</p>}
        <button type="submit" disabled={submitting}>
          {submitting ? "안전하게 설정하는 중…" : "최초 신뢰 기기 등록"}
        </button>
      </form>
    </main>
  );
}

function formatDuration(totalSeconds: number) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  return hours > 0 ? `${hours}시간 ${minutes}분` : `${minutes}분 ${seconds}초`;
}

function readCookie(name: string) {
  const prefix = `${encodeURIComponent(name)}=`;
  const value = document.cookie.split("; ").find((item) => item.startsWith(prefix));
  return value ? decodeURIComponent(value.slice(prefix.length)) : "";
}
