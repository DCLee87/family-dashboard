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
  type: string;
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
        fetchDeviceWithRefresh(),
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
    return <DeviceEnrollment type="parent_mobile" />;
  }

  if (window.location.pathname === "/enroll/tablet") {
    return <DeviceEnrollment type="shared_tablet" />;
  }

  if (setupRequired) {
    return <SetupScreen onComplete={() => setSetupRequired(false)} />;
  }

  const healthy = health.status === "ok" && health.database === "ok";

  return (
    <main>
      <header>
        <p className="eyebrow">C1 · FAMILY SCHEDULE</p>
        <h1>우리 가족 대시보드</h1>
        <p className="subtitle">등록된 가족 기기에서 오늘의 일정과 NAS 서비스 상태를 함께 확인합니다.</p>
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
      <NotificationControl auth={deviceAuth} />
      <ScheduleBoard auth={deviceAuth} />

      <footer>
        마지막 확인 {checkedAt ? checkedAt.toLocaleTimeString("ko-KR") : "대기 중"} · 30초마다 자동 갱신
      </footer>
    </main>
  );
}

type FamilyMember = {
  id: string;
  displayName: string;
  role: string;
};

type ScheduleOccurrence = {
  id?: string;
  title: string;
  locationName?: string;
  notes?: string;
  visibility?: string;
  startsAt?: string;
  endsAt?: string;
  participants?: string[];
  version?: number;
  summary?: boolean;
  overlap?: boolean;
  occurrenceKey?: string;
  recurring?: boolean;
  occurrenceVersion?: number;
  timeKind?: string;
  startDate?: string;
  endDate?: string;
};

type FamilyStatusItem = {
  member: FamilyMember;
  status: ScheduleOccurrence | null;
  source: string;
};

type ScheduleForm = {
  id: string;
  title: string;
  locationName: string;
  visibility: string;
  startsAt: string;
  endsAt: string;
  participants: string[];
  version: number;
  weeklyRepeat: boolean;
  weekdays: number[];
  recurrenceEndsOn: string;
  occurrenceKey: string;
  occurrenceVersion: number;
  allDay: boolean;
  startDate: string;
  endDate: string;
};

function emptyScheduleForm(): ScheduleForm {
  const start = new Date();
  start.setMinutes(Math.ceil(start.getMinutes() / 30) * 30, 0, 0);
  const end = new Date(start.getTime() + 60 * 60 * 1000);
	const today = toLocalInput(start).slice(0, 10);
  return {
    id: "", title: "", locationName: "", visibility: "family",
    startsAt: toLocalInput(start), endsAt: toLocalInput(end),
    participants: [], version: 0, weeklyRepeat: false,
    weekdays: [start.getDay()], recurrenceEndsOn: "",
    occurrenceKey: "", occurrenceVersion: 0,
    allDay: false, startDate: today, endDate: today,
  };
}

function ScheduleBoard({ auth }: { auth: DeviceAuth | null }) {
  const [members, setMembers] = useState<FamilyMember[]>([]);
  const [occurrences, setOccurrences] = useState<ScheduleOccurrence[]>([]);
  const [familyStatuses, setFamilyStatuses] = useState<FamilyStatusItem[]>([]);
  const [selectedMember, setSelectedMember] = useState("");
  const [form, setForm] = useState<ScheduleForm>(emptyScheduleForm);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [showForm, setShowForm] = useState(false);

  const refreshSchedules = useCallback(async () => {
    if (!auth) {
      setMembers([]);
      setOccurrences([]);
      setFamilyStatuses([]);
      return;
    }
    const today = new Date();
    const from = new Date(today.getFullYear(), today.getMonth(), today.getDate());
    const to = new Date(from);
    to.setDate(to.getDate() + 1);
    try {
      const [memberResponse, scheduleResponse, statusResponse] = await Promise.all([
        fetch("/api/v1/family-members", { cache: "no-store" }),
        fetch(`/api/v1/schedule-occurrences?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`, { cache: "no-store" }),
        fetch("/api/v1/family-status", { cache: "no-store" }),
      ]);
      if (!memberResponse.ok || !scheduleResponse.ok || !statusResponse.ok) {
        throw new Error("오늘 일정과 가족 상태를 불러오지 못했습니다.");
      }
      const memberResult = await memberResponse.json() as { members: FamilyMember[] };
      const scheduleResult = await scheduleResponse.json() as { occurrences: ScheduleOccurrence[] };
      const statusResult = await statusResponse.json() as { members: FamilyStatusItem[] };
      setMembers(memberResult.members);
      setOccurrences(scheduleResult.occurrences);
      setFamilyStatuses(statusResult.members);
      setError("");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "오늘 일정과 가족 상태를 불러오지 못했습니다.");
    }
  }, [auth]);

  useEffect(() => {
    void refreshSchedules();
  }, [refreshSchedules]);

  if (!auth) return null;

  const filteredOccurrences = selectedMember
    ? occurrences.filter((item) => item.participants?.includes(selectedMember))
    : occurrences;

  function toggleParticipant(id: string) {
    setForm((current) => ({
      ...current,
      participants: current.participants.includes(id)
        ? current.participants.filter((participant) => participant !== id)
        : [...current.participants, id],
    }));
  }

  function editSchedule(item: ScheduleOccurrence) {
    if (!item.id) return;
    const fallbackStart = new Date();
    const fallbackEnd = new Date(fallbackStart.getTime() + 60 * 60 * 1000);
    setForm({
      id: item.id,
      title: item.title,
      locationName: item.locationName ?? "",
      visibility: item.visibility ?? "family",
      startsAt: toLocalInput(item.startsAt ? new Date(item.startsAt) : fallbackStart),
      endsAt: toLocalInput(item.endsAt ? new Date(item.endsAt) : fallbackEnd),
      participants: item.participants ?? [],
      version: item.version ?? 1,
      weeklyRepeat: false,
      weekdays: [(item.startsAt ? new Date(item.startsAt) : fallbackStart).getDay()],
      recurrenceEndsOn: "",
      occurrenceKey: item.occurrenceKey ?? "",
      occurrenceVersion: item.occurrenceVersion ?? 0,
      allDay: item.timeKind === "all_day",
      startDate: item.startDate ?? "",
      endDate: item.endDate ?? "",
    });
    setShowForm(true);
  }

  async function save(confirmOverlap = false) {
    setBusy(true);
    setError("");
    try {
      if (!form.title.trim()) throw new Error("일정 제목을 입력하세요.");
      if (form.participants.length === 0) throw new Error("대상 가족을 한 명 이상 선택하세요.");
      if (!form.id && form.weeklyRepeat && form.weekdays.length === 0) throw new Error("반복 요일을 한 개 이상 선택하세요.");
      const method = form.id ? "PUT" : "POST";
      const target = form.id
        ? form.occurrenceKey
          ? `/api/v1/schedules/${encodeURIComponent(form.id)}/occurrences/${encodeURIComponent(form.occurrenceKey)}`
          : `/api/v1/schedules/${encodeURIComponent(form.id)}`
        : "/api/v1/schedules";
      const response = await fetch(target, {
        method,
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": readCookie("family_dashboard_csrf"),
        },
        body: JSON.stringify({
          title: form.title,
          locationName: form.locationName,
          visibility: form.visibility,
          startsAt: new Date(form.startsAt).toISOString(),
          endsAt: new Date(form.endsAt).toISOString(),
          timeKind: form.allDay ? "all_day" : "timed",
          startDate: form.allDay ? form.startDate : undefined,
          endDate: form.allDay ? form.endDate : undefined,
          participants: form.participants,
          version: form.version,
          occurrenceVersion: form.occurrenceVersion,
          confirmOverlap,
          recurrence: !form.id && form.weeklyRepeat ? {
            kind: "weekly",
            weekdays: form.weekdays,
            endsOn: form.recurrenceEndsOn || undefined,
          } : undefined,
        }),
      });
      const result = await response.json() as { code?: string; message?: string };
      if (response.status === 409 && result.code === "overlap_warning" && !confirmOverlap) {
        if (window.confirm("같은 가족에게 겹치는 일정이 있습니다. 그래도 저장할까요?")) {
          await save(true);
        }
        return;
      }
      if (!response.ok) throw new Error(result.message ?? "일정을 저장하지 못했습니다.");
      setForm(emptyScheduleForm());
      setShowForm(false);
      await refreshSchedules();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "일정을 저장하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function cancelOccurrence() {
    if (!form.id || !form.occurrenceKey) return;
    if (!window.confirm("이 반복 일정의 이번 회차만 취소할까요?")) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        `/api/v1/schedules/${encodeURIComponent(form.id)}/occurrences/${encodeURIComponent(form.occurrenceKey)}/cancel`,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": readCookie("family_dashboard_csrf"),
          },
          body: JSON.stringify({ occurrenceVersion: form.occurrenceVersion }),
        },
      );
      if (!response.ok) {
        const result = await response.json() as { message?: string };
        throw new Error(result.message ?? "이번 회차를 취소하지 못했습니다.");
      }
      setForm(emptyScheduleForm());
      setShowForm(false);
      await refreshSchedules();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "이번 회차를 취소하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="schedule-card">
      <div className="schedule-heading">
        <div>
          <p className="label">오늘의 일정</p>
          <strong>{occurrences.length === 0 ? "등록된 일정 없음" : `${occurrences.length}개 일정`}</strong>
        </div>
        {auth.permissions.admin && (
          <button type="button" onClick={() => {
            setForm(emptyScheduleForm());
            setShowForm(form.id ? true : !showForm);
          }}>
            {form.id ? "새 일정" : showForm ? "입력 닫기" : "일정 추가"}
          </button>
        )}
      </div>

      <div className="family-status-grid" aria-label="일정 기반 가족 상태">
        {familyStatuses.map((item) => (
          <article key={item.member.id}>
            <p className="label">{item.member.displayName}</p>
            <strong>
              {item.status
                ? item.status.locationName || item.status.title
                : "일정 없음"}
            </strong>
            <span>
              {item.status
                ? item.status.endsAt
                  ? `${item.source} · ${formatStatusEnd(item.status.endsAt)}까지`
                  : item.source
                : "현재 적용 일정 없음"}
            </span>
            {item.status?.overlap && <span className="overlap-badge">일정 겹침</span>}
          </article>
        ))}
      </div>

      <div className="schedule-filters" aria-label="가족별 일정 필터">
        <button
          type="button"
          className={selectedMember === "" ? "filter-active" : "secondary"}
          aria-pressed={selectedMember === ""}
          onClick={() => setSelectedMember("")}
        >
          전체
        </button>
        {members.map((member) => (
          <button
            key={member.id}
            type="button"
            className={selectedMember === member.id ? "filter-active" : "secondary"}
            aria-pressed={selectedMember === member.id}
            onClick={() => setSelectedMember(member.id)}
          >
            {member.displayName}
          </button>
        ))}
      </div>

      {showForm && !form.id && auth.permissions.admin && (
        <ScheduleEditor
          form={form}
          members={members}
          busy={busy}
          onChange={setForm}
          onToggleParticipant={toggleParticipant}
          onSave={() => void save()}
          onCancel={() => {
            setForm(emptyScheduleForm());
            setShowForm(false);
          }}
        />
      )}

      {error && <p className="form-error">{error}</p>}
      <div className="schedule-list">
        {filteredOccurrences.map((item, index) => {
          const key = item.occurrenceKey ? `${item.id}-${item.occurrenceKey}` : item.id ?? `${item.startsAt}-${index}`;
          if (item.id && item.id === form.id &&
            (form.occurrenceKey ? item.occurrenceKey === form.occurrenceKey : !item.occurrenceKey) &&
            auth.permissions.admin) {
            return (
              <article key={key} className="schedule-editor-row">
                <ScheduleEditor
                  form={form}
                  members={members}
                  busy={busy}
                  onChange={setForm}
                  onToggleParticipant={toggleParticipant}
                  onSave={() => void save()}
                  onCancelOccurrence={form.occurrenceKey ? () => void cancelOccurrence() : undefined}
                  onCancel={() => {
                    setForm(emptyScheduleForm());
                    setShowForm(false);
                  }}
                />
              </article>
            );
          }
          return (
            <article key={key} className={item.summary ? "schedule-summary" : ""}>
              <div>
                <time>{item.timeKind === "all_day" ? formatAllDay(item.startDate, item.endDate) : formatScheduleTime(item.startsAt!, item.endsAt!)}</time>
                <strong>{item.title}</strong>
                {item.recurring && <span className="recurrence-badge">매주 반복</span>}
                {item.locationName && <span>{item.locationName}</span>}
                {item.overlap && <span className="overlap-badge">일정 겹침</span>}
              </div>
              {auth.permissions.admin && item.id && (
                <button type="button" className="secondary" onClick={() => editSchedule(item)}>
                  {item.recurring ? "이번 회차 수정" : "수정"}
                </button>
              )}
            </article>
          );
        })}
        {filteredOccurrences.length === 0 && (
          <p className="schedule-empty">
            {selectedMember ? "선택한 가족의 오늘 일정이 없습니다." : "오늘 등록된 일정이 없습니다."}
          </p>
        )}
      </div>
    </section>
  );
}

function ScheduleEditor({
  form,
  members,
  busy,
  onChange,
  onToggleParticipant,
  onSave,
  onCancel,
  onCancelOccurrence,
}: {
  form: ScheduleForm;
  members: FamilyMember[];
  busy: boolean;
  onChange: (form: ScheduleForm) => void;
  onToggleParticipant: (id: string) => void;
  onSave: () => void;
  onCancel: () => void;
  onCancelOccurrence?: () => void;
}) {
  return (
    <form className="schedule-form" onSubmit={(event) => {
      event.preventDefault();
      onSave();
    }}>
      <label>
        <span>제목</span>
        <input value={form.title} maxLength={200} onChange={(event) => onChange({ ...form, title: event.target.value })} />
      </label>
      <label>
        <span>장소</span>
        <input value={form.locationName} maxLength={200} onChange={(event) => onChange({ ...form, locationName: event.target.value })} />
      </label>
      {!form.id && (
        <label className="recurrence-toggle">
          <input type="checkbox" checked={form.allDay} onChange={(event) => onChange({ ...form, allDay: event.target.checked, weeklyRepeat: false })} />
          <span>종일 일정</span>
        </label>
      )}
      {form.allDay ? (
        <div className="schedule-time-fields">
          <label>
            <span>시작일</span>
            <input type="date" value={form.startDate} onChange={(event) => onChange({ ...form, startDate: event.target.value })} />
          </label>
          <label>
            <span>종료일 (포함)</span>
            <input type="date" min={form.startDate} value={form.endDate} onChange={(event) => onChange({ ...form, endDate: event.target.value })} />
          </label>
        </div>
      ) : <div className="schedule-time-fields">
        <label>
          <span>시작</span>
          <input type="datetime-local" value={form.startsAt} onChange={(event) => onChange({ ...form, startsAt: event.target.value })} />
        </label>
        <label>
          <span>종료</span>
          <input type="datetime-local" value={form.endsAt} onChange={(event) => onChange({ ...form, endsAt: event.target.value })} />
        </label>
      </div>}
      {!form.id && !form.allDay && (
        <fieldset>
          <legend>반복</legend>
          <label className="recurrence-toggle">
            <input
              type="checkbox"
              checked={form.weeklyRepeat}
              onChange={(event) => onChange({ ...form, weeklyRepeat: event.target.checked })}
            />
            <span>매주 반복</span>
          </label>
          {form.weeklyRepeat && (
            <>
              <div className="participant-options" aria-label="반복 요일">
                {[[0, "일"], [1, "월"], [2, "화"], [3, "수"], [4, "목"], [5, "금"], [6, "토"]].map(([day, label]) => (
                  <label key={day}>
                    <input
                      type="checkbox"
                      checked={form.weekdays.includes(day as number)}
                      onChange={() => onChange({
                        ...form,
                        weekdays: form.weekdays.includes(day as number)
                          ? form.weekdays.filter((value) => value !== day)
                          : [...form.weekdays, day as number],
                      })}
                    />
                    <span>{label}</span>
                  </label>
                ))}
              </div>
              <label>
                <span>반복 종료일 (선택)</span>
                <input
                  type="date"
                  value={form.recurrenceEndsOn}
                  min={form.startsAt.slice(0, 10)}
                  onChange={(event) => onChange({ ...form, recurrenceEndsOn: event.target.value })}
                />
              </label>
            </>
          )}
        </fieldset>
      )}
      <label>
        <span>공개 범위</span>
        <select value={form.visibility} onChange={(event) => onChange({ ...form, visibility: event.target.value })}>
          <option value="family">가족 공개</option>
          <option value="tv_summary">TV 요약</option>
          <option value="parents_only">부모 전용</option>
        </select>
      </label>
      <fieldset>
        <legend>대상 가족</legend>
        <div className="participant-options">
          {members.map((member) => (
            <label key={member.id}>
              <input
                type="checkbox"
                checked={form.participants.includes(member.id)}
                onChange={() => onToggleParticipant(member.id)}
              />
              <span>{member.displayName}</span>
            </label>
          ))}
        </div>
      </fieldset>
      <div className="button-row">
        <button type="submit" disabled={busy}>{busy ? "저장 중…" : form.occurrenceKey ? "이번 회차 저장" : form.id ? "일정 수정" : "일정 저장"}</button>
        {onCancelOccurrence && (
          <button type="button" className="danger" disabled={busy} onClick={onCancelOccurrence}>이번 회차 취소</button>
        )}
        <button type="button" className="secondary" disabled={busy} onClick={onCancel}>취소</button>
      </div>
    </form>
  );
}

function toLocalInput(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000;
  return new Date(value.getTime() - offset).toISOString().slice(0, 16);
}

function formatScheduleTime(startsAt: string, endsAt: string): string {
  const format = new Intl.DateTimeFormat("ko-KR", { hour: "2-digit", minute: "2-digit" });
  return `${format.format(new Date(startsAt))}–${format.format(new Date(endsAt))}`;
}

function formatAllDay(startDate?: string, endDate?: string): string {
  if (!startDate || !endDate || startDate === endDate) return "종일";
  return `종일 · ${startDate.slice(5)}–${endDate.slice(5)}`;
}

function formatStatusEnd(endsAt: string): string {
  return new Intl.DateTimeFormat("ko-KR", {
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(endsAt));
}

type PushConfig = {
  enabled: boolean;
  eligible: boolean;
  subscribed: boolean;
  publicKey: string;
};

function NotificationControl({ auth }: { auth: DeviceAuth | null }) {
  const [config, setConfig] = useState<PushConfig | null>(null);
  const [permission, setPermission] = useState<NotificationPermission>(
    "Notification" in window ? Notification.permission : "denied",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const refreshConfig = useCallback(async () => {
    if (!auth || !["parent_mobile", "shared_tablet"].includes(auth.device.type)) {
      setConfig(null);
      return;
    }
    const response = await fetch("/api/push/config", { cache: "no-store" });
    if (response.ok) setConfig(await response.json());
  }, [auth]);

  useEffect(() => {
    void refreshConfig();
  }, [refreshConfig]);

  if (!auth || !config?.eligible) return null;

  async function enable() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      if (!config?.enabled) throw new Error("서버의 알림 키 설정이 아직 완료되지 않았습니다.");
      if (!("serviceWorker" in navigator) || !("PushManager" in window)) {
        throw new Error("이 브라우저는 Web Push를 지원하지 않습니다.");
      }
      const result = await Notification.requestPermission();
      setPermission(result);
      if (result !== "granted") {
        throw new Error("알림 권한이 허용되지 않았습니다. 브라우저 사이트 설정에서 변경할 수 있습니다.");
      }
      const registration = await navigator.serviceWorker.ready;
      const existing = await registration.pushManager.getSubscription();
      const subscription = existing ?? await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeBase64URL(config.publicKey),
      });
      const response = await fetch("/api/push/subscription", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(subscription.toJSON()),
      });
      if (!response.ok) throw new Error("알림 구독을 서버에 저장하지 못했습니다.");
      await refreshConfig();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "알림을 활성화하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function disable() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      if ("serviceWorker" in navigator) {
        const registration = await navigator.serviceWorker.ready;
        const subscription = await registration.pushManager.getSubscription();
        if (subscription) await subscription.unsubscribe();
      }
      const response = await fetch("/api/push/subscription", { method: "DELETE" });
      if (!response.ok) throw new Error("알림 구독을 해제하지 못했습니다.");
      await refreshConfig();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "알림을 해제하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function sendTest() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const response = await fetch("/api/push/test", {
        method: "POST",
        headers: { "X-CSRF-Token": readCookie("family_dashboard_csrf") },
      });
      if (!response.ok) throw new Error("테스트 알림을 보내지 못했습니다.");
      setMessage("테스트 알림을 전송했습니다.");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "테스트 알림을 보내지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  const status = config.subscribed && permission === "granted"
    ? "활성화됨"
    : permission === "denied"
      ? "브라우저에서 차단됨"
      : "비활성화";

  return (
    <section className="notification-card">
      <div>
        <p className="label">모바일 알림</p>
        <strong>{status}</strong>
        <p>
          {auth.device.type === "shared_tablet"
            ? "공용 태블릿 알림은 선택 기능이며 기본적으로 꺼져 있습니다."
            : "일정과 할 일 알림을 받으려면 이 기기에서 한 번 허용하세요."}
        </p>
      </div>
      {config.subscribed ? (
        <div className="button-row">
          {auth.permissions.admin && (
            <button type="button" disabled={busy} onClick={() => void sendTest()}>
              테스트 알림
            </button>
          )}
          <button type="button" className="secondary" disabled={busy} onClick={() => void disable()}>
            {busy ? "처리 중…" : "알림 해제"}
          </button>
        </div>
      ) : (
        <button type="button" disabled={busy || permission === "denied"} onClick={() => void enable()}>
          {busy ? "처리 중…" : "알림 활성화"}
        </button>
      )}
      {message && <p className="form-success" role="status">{message}</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
    </section>
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
          <div className="enrollment-links" aria-label="기기 등록">
            <a className="enrollment-link primary" href="/enroll">부모 휴대폰 등록</a>
            <a className="enrollment-link secondary" href="/enroll/tablet">공용 태블릿 등록</a>
          </div>
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
  const [code, setCode] = useState<{ value: string; type: string } | null>(null);
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

  async function createEnrollment(type: "parent_mobile" | "shared_tablet") {
    setError("");
    const response = await fetch("/api/admin/enrollments", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": readCookie("family_dashboard_csrf"),
      },
      body: JSON.stringify({ type }),
    });
    if (!response.ok) {
      setError("기기 등록 코드를 만들지 못했습니다.");
      return;
    }
    const result = await response.json();
    setCode({ value: result.code, type: result.type });
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
          <p className="label">새 기기 등록</p>
          <p>등록할 기기에서 일회용 코드를 입력한 뒤 여기서 승인하세요.</p>
        </div>
        <div className="button-row">
          <button type="button" onClick={() => void createEnrollment("parent_mobile")}>부모 모바일</button>
          <button type="button" onClick={() => void createEnrollment("shared_tablet")}>공용 태블릿</button>
        </div>
      </div>
      {code && (
        <div className="enrollment-code">
          <strong>{code.value}</strong>
          <span>10분 동안 한 번만 사용할 수 있습니다.</span>
          <a href={code.type === "shared_tablet" ? "/enroll/tablet" : "/enroll"}
            target="_blank" rel="noreferrer">
            {code.type === "shared_tablet" ? "태블릿" : "모바일"} 등록 화면 열기
          </a>
        </div>
      )}
      {enrollments.filter((item) => item.status === "submitted").map((item) => (
        <div className="enrollment-request" key={item.id}>
          <div>
            <strong>{item.name}</strong>
            <span>
              {item.type === "shared_tablet"
                ? "공용 태블릿 등록 요청"
                : `${item.owner === "dad" ? "아빠" : "엄마"} 모바일 등록 요청`}
            </span>
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
                {device.type === "trusted_pc" ? "신뢰 PC"
                  : device.type === "shared_tablet" ? "공용 태블릿"
                    : device.type === "tv" ? "TV" : "부모 모바일"}
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

function DeviceEnrollment({ type }: { type: "parent_mobile" | "shared_tablet" }) {
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
      body: JSON.stringify({ code, deviceName, owner: type === "parent_mobile" ? owner : "", type }),
    });
    if (!response.ok) {
      const result = await response.json().catch(() => ({ status: "" }));
      setError(response.status === 403 && result.status === "local_network_required"
        ? "새 기기 등록은 집 Wi-Fi 또는 부모용 Tailscale 주소에서 진행하세요."
        : "등록 코드가 잘못되었거나 만료되었습니다.");
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
        <p className="eyebrow">{type === "shared_tablet" ? "E2 · SHARED TABLET" : "E2 · PARENT MOBILE"}</p>
        <h1>{type === "shared_tablet" ? "공용 태블릿 등록" : "부모 모바일 등록"}</h1>
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
          <input maxLength={80}
            placeholder={type === "shared_tablet" ? "예: 거실 태블릿" : "예: 아빠 iPhone"} value={deviceName}
            onChange={(event) => setDeviceName(event.target.value)} disabled={Boolean(claimToken)} required />
        </label>
        {type === "parent_mobile" && (
          <label>
            <span>사용자</span>
            <select value={owner} onChange={(event) => setOwner(event.target.value)} disabled={Boolean(claimToken)}>
              <option value="dad">아빠</option>
              <option value="mom">엄마</option>
            </select>
          </label>
        )}
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

function decodeBase64URL(value: string) {
  const padding = "=".repeat((4 - value.length % 4) % 4);
  const base64 = (value + padding).replace(/-/g, "+").replace(/_/g, "/");
  const binary = window.atob(base64);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

async function fetchDeviceWithRefresh() {
  let response = await fetch("/api/auth/device", { cache: "no-store" });
  if (response.status !== 401) return response;
  const refreshed = await fetch("/api/auth/refresh", { method: "POST" });
  if (!refreshed.ok) return response;
  response = await fetch("/api/auth/device", { cache: "no-store" });
  return response;
}
