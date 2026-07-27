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
    id: string;
    name: string;
    type: string;
  };
  permissions: {
    view: boolean;
    admin: boolean;
    canUnlockAdmin: boolean;
  };
};

type EnrollmentItem = {
  id: string;
  name: string;
  owner: string;
  status: string;
  expiresAt: string;
};

type RegisteredDevice = {
  id: string;
  name: string;
  type: string;
  owner: string;
  status: string;
  lastUsedAt: string | null;
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

  if (window.location.pathname === "/enroll") {
    return <MobileEnrollment />;
  }

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
      {auth.permissions.admin && auth.device.type === "trusted_pc" && (
        <DeviceManagement currentDeviceId={auth.device.id} />
      )}
      {error && <p className="form-error" role="alert">{error}</p>}
    </section>
  );
}

function DeviceManagement({ currentDeviceId }: { currentDeviceId: string }) {
  const [code, setCode] = useState("");
  const [enrollments, setEnrollments] = useState<EnrollmentItem[]>([]);
  const [devices, setDevices] = useState<RegisteredDevice[]>([]);
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    const [enrollmentResponse, deviceResponse] = await Promise.all([
      fetch("/api/admin/enrollments", { cache: "no-store" }),
      fetch("/api/admin/devices", { cache: "no-store" }),
    ]);
    if (!enrollmentResponse.ok || !deviceResponse.ok) return;
    setEnrollments((await enrollmentResponse.json()).enrollments);
    setDevices((await deviceResponse.json()).devices);
  }, []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 3_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  async function createEnrollment() {
    setError("");
    const response = await adminFetch("/api/admin/enrollments");
    if (!response.ok) {
      setError("모바일 등록 코드를 만들지 못했습니다.");
      return;
    }
    const result = await response.json();
    setCode(result.code);
    await refresh();
  }

  async function approve(id: string) {
    const response = await adminFetch(`/api/admin/enrollments/${encodeURIComponent(id)}/approve`);
    if (!response.ok) {
      setError("등록 요청을 승인하지 못했습니다.");
      return;
    }
    await refresh();
  }

  async function reject(id: string) {
    const response = await adminFetch(`/api/admin/enrollments/${encodeURIComponent(id)}/reject`);
    if (!response.ok) {
      setError("등록 요청을 거절하지 못했습니다.");
      return;
    }
    await refresh();
  }

  async function revoke(id: string, name: string) {
    if (!window.confirm(`${name} 기기의 접속 권한을 폐기할까요?`)) return;
    const response = await adminFetch(`/api/admin/devices/${encodeURIComponent(id)}/revoke`);
    if (!response.ok) {
      setError("기기 권한을 폐기하지 못했습니다.");
      return;
    }
    await refresh();
  }

  return (
    <div className="device-management">
      <div className="management-heading">
        <div>
          <p className="label">부모 모바일 등록</p>
          <p>모바일에서 등록 화면을 열고 일회용 코드를 입력한 뒤 여기서 승인하세요.</p>
        </div>
        <button type="button" onClick={() => void createEnrollment()}>등록 코드 만들기</button>
      </div>
      {code && (
        <div className="enrollment-code">
          <strong>{code}</strong>
          <span>10분 동안 한 번만 사용할 수 있습니다.</span>
          <a href="/enroll" target="_blank" rel="noreferrer">모바일 등록 화면 열기</a>
        </div>
      )}
      {enrollments.filter((item) => item.status === "submitted").map((item) => (
        <div className="enrollment-request" key={item.id}>
          <div>
            <strong>{item.name}</strong>
            <span>{item.owner === "dad" ? "아빠" : "엄마"} 모바일 등록 요청</span>
          </div>
          <div className="button-row">
            <button type="button" onClick={() => void approve(item.id)}>승인</button>
            <button type="button" className="danger" onClick={() => void reject(item.id)}>거절</button>
          </div>
        </div>
      ))}
      <div className="device-list">
        <p className="label">등록 기기</p>
        {devices.map((device) => (
          <div className="device-row" key={device.id}>
            <div>
              <strong>{device.name}</strong>
              <span>
                {device.type === "trusted_pc" ? "신뢰 PC" : "부모 모바일"}
                {device.owner ? ` · ${device.owner === "dad" ? "아빠" : "엄마"}` : ""}
                {device.status === "revoked" ? " · 폐기됨" : ""}
              </span>
            </div>
            {device.status === "active" && device.id !== currentDeviceId && (
              <button type="button" className="danger" onClick={() => void revoke(device.id, device.name)}>
                폐기
              </button>
            )}
          </div>
        ))}
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
    </div>
  );
}

function MobileEnrollment() {
  const [code, setCode] = useState("");
  const [deviceName, setDeviceName] = useState("");
  const [owner, setOwner] = useState("dad");
  const [claimToken, setClaimToken] = useState(() => sessionStorage.getItem("family_dashboard_claim") || "");
  const [status, setStatus] = useState(claimToken ? "PC 승인을 기다리고 있습니다." : "");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!claimToken) return;
    let stopped = false;
    async function claim() {
      const response = await fetch("/api/enrollments/claim", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ claimToken }),
      });
      if (stopped) return;
      if (response.status === 201) {
        sessionStorage.removeItem("family_dashboard_claim");
        setStatus("등록이 완료되었습니다. 대시보드로 이동합니다.");
        window.setTimeout(() => window.location.assign("/"), 600);
      } else if (response.status !== 202) {
        const result = await response.json().catch(() => ({ status: "" }));
        sessionStorage.removeItem("family_dashboard_claim");
        setClaimToken("");
        setError(result.status === "rejected"
          ? "신뢰 PC에서 등록 요청을 거절했습니다."
          : "등록 요청이 만료되었거나 더 이상 유효하지 않습니다.");
      }
    }
    void claim();
    const timer = window.setInterval(() => void claim(), 2_000);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [claimToken]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    const response = await fetch("/api/enrollments/submit", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, deviceName, owner }),
    });
    if (!response.ok) {
      setError("등록 코드가 잘못되었거나 만료되었습니다.");
      return;
    }
    const result = await response.json();
    sessionStorage.setItem("family_dashboard_claim", result.claimToken);
    setClaimToken(result.claimToken);
    setCode("");
    setStatus("PC 승인을 기다리고 있습니다.");
  }

  return (
    <main className="setup-layout mobile-enrollment">
      <header className="setup-intro">
        <p className="eyebrow">E2 · PARENT MOBILE</p>
        <h1>부모 모바일 등록</h1>
        <p className="subtitle">신뢰 PC에서 만든 일회용 코드를 입력하세요. PC의 최종 승인 후 이 기기만의 인증정보가 발급됩니다.</p>
      </header>
      <form className="setup-card" onSubmit={submit}>
        <label>
          <span>8자리 등록 코드</span>
          <input inputMode="numeric" pattern="[0-9]{8}" maxLength={8} value={code}
            onChange={(event) => setCode(event.target.value)} disabled={Boolean(claimToken)} required />
        </label>
        <label>
          <span>기기 이름</span>
          <input maxLength={80} placeholder="예: 아빠 iPhone" value={deviceName}
            onChange={(event) => setDeviceName(event.target.value)} disabled={Boolean(claimToken)} required />
        </label>
        <label>
          <span>사용자</span>
          <select value={owner} onChange={(event) => setOwner(event.target.value)} disabled={Boolean(claimToken)}>
            <option value="dad">아빠</option>
            <option value="mom">엄마</option>
          </select>
        </label>
        {status && <p className="waiting-status" role="status">{status}</p>}
        {error && <p className="form-error" role="alert">{error}</p>}
        {!claimToken && <button type="submit">등록 요청 보내기</button>}
      </form>
    </main>
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

function adminFetch(path: string) {
  return fetch(path, {
    method: "POST",
    headers: { "X-CSRF-Token": readCookie("family_dashboard_csrf") },
  });
}
