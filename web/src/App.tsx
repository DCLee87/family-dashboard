import { FormEvent, useCallback, useEffect, useState } from "react";
import { C5Dashboard, DashboardPreferences } from "./C5Dashboard";

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

export type DeviceAuth = {
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
    canManageDevices: boolean;
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
type AppTab = "dad"|"mom"|"daughter"|"ledger"|"investment"|"settings";
const appTabs:Array<{id:AppTab;label:string;icon:string}>=[
  {id:"dad",label:"아빠",icon:"아"},{id:"mom",label:"엄마",icon:"엄"},{id:"daughter",label:"딸",icon:"딸"},{id:"ledger",label:"가계부",icon:"₩"},{id:"investment",label:"재테크",icon:"↗"},{id:"settings",label:"설정",icon:"⚙"},
];
const familyTabLabels:Record<"dad"|"mom"|"daughter",string>={dad:"아빠",mom:"엄마",daughter:"딸"};

export default function App() {
  const [health, setHealth] = useState<Health>(initialHealth);
  const [runtime, setRuntime] = useState<Runtime | null>(null);
  const [checkedAt, setCheckedAt] = useState<Date | null>(null);
  const [lastSuccessfulAt, setLastSuccessfulAt] = useState<Date | null>(null);
  const [online, setOnline] = useState(navigator.onLine);
  const [setupRequired, setSetupRequired] = useState<boolean | null>(null);
  const [deviceAuth, setDeviceAuth] = useState<DeviceAuth | null>(null);
  const [activeTab,setActiveTab]=useState<AppTab>(()=>{const saved=localStorage.getItem("family-dashboard-tab");return appTabs.some(tab=>tab.id===saved)?saved as AppTab:"dad"});

  useEffect(()=>{localStorage.setItem("family-dashboard-tab",activeTab)},[activeTab]);

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
      setLastSuccessfulAt(new Date());
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

  useEffect(() => {
    const handleOnline = () => {
      setOnline(true);
      void refresh();
    };
    const handleOffline = () => setOnline(false);
    const handleVisibility = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    document.addEventListener("visibilitychange", handleVisibility);
    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [refresh]);

  if (window.location.pathname === "/enroll") {
    return <DeviceEnrollment type="parent_mobile" />;
  }

  if (window.location.pathname === "/enroll/tablet") {
    return <DeviceEnrollment type="shared_tablet" />;
  }

  if (setupRequired) {
    return <SetupScreen onComplete={() => void refresh()} />;
  }

  if (setupRequired === false && !deviceAuth) {
    return <ParentLogin onComplete={refresh} />;
  }

  const healthy = health.status === "ok" && health.database === "ok";

  const familyMember=activeTab==="dad"||activeTab==="mom"||activeTab==="daughter"?activeTab:null;
  const activeLabel=appTabs.find(tab=>tab.id===activeTab)?.label||"대시보드";

  return (
    <main className="tab-app">
      <header className="app-header">
        <p className="eyebrow">{(runtime?.version || "FAMILY").toUpperCase()} · FAMILY SCHEDULE</p>
        <h1>{activeLabel}</h1>
        <p className="subtitle">{familyMember?`${familyTabLabels[familyMember]}의 오늘 일정과 할 일을 확인합니다.`:activeTab==="ledger"?"매월 21일 월급을 기준으로 우리 가족의 생활 예산을 관리합니다.":activeTab==="investment"?"주식과 장기 자산을 생활비와 분리해 살펴봅니다.":"기기, 알림과 서비스 상태를 관리합니다."}</p>
      </header>

      {familyMember&&<section className="tab-content" aria-label={`${familyTabLabels[familyMember]} 탭`}><C5Dashboard auth={deviceAuth} showPreferences={false}
        schedule={<ScheduleBoard auth={deviceAuth} focusMemberId={familyMember} weekly={familyMember==="daughter"} />}
        tasks={<TaskBoard auth={deviceAuth} focusMemberId={familyMember} />} /></section>}

      {activeTab==="ledger"&&<LedgerDashboard auth={deviceAuth}/>}

      {activeTab==="investment"&&<InvestmentDashboard auth={deviceAuth}/>}

      {activeTab==="settings"&&<section className="settings-tab" aria-label="설정 탭"><section className={`status-card ${healthy && online ? "healthy" : "unhealthy"}`}>
        <div>
          <span className="status-dot" aria-hidden="true" />
          <p className="label">서비스 상태</p>
          <strong>{!online ? "오프라인" : health.status === "loading" ? "확인 중" : healthy ? "정상" : "연결 필요"}</strong>
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
      <DashboardPreferences auth={deviceAuth}/>
      <FamilyInviteGuide />

      <footer>
        마지막 확인 {checkedAt ? checkedAt.toLocaleTimeString("ko-KR") : "대기 중"}
        {lastSuccessfulAt && !healthy ? ` · 마지막 정상 연결 ${lastSuccessfulAt.toLocaleTimeString("ko-KR")}` : ""}
        {` · ${online ? "30초마다 자동 갱신" : "연결 복구 시 자동 재시도"}`}
      </footer></section>}
      <nav className="bottom-tabs" aria-label="가족 대시보드 탭">{appTabs.map(tab=><button type="button" key={tab.id} className={activeTab===tab.id?"active":""} aria-current={activeTab===tab.id?"page":undefined} onClick={()=>{setActiveTab(tab.id);window.scrollTo({top:0,behavior:"smooth"})}}><span aria-hidden="true">{tab.icon}</span><strong>{tab.label}</strong></button>)}</nav>
    </main>
  );
}

function FamilyInviteGuide(){return <section className="invite-guide-card"><div className="invite-guide-heading"><div><p className="label">가족 휴대폰 초대</p><strong>앱 설치 없이 배우자 초대하기</strong></div></div><ol><li><strong>엄마 로그인 비밀번호 설정</strong><span>관리자 모드를 연 뒤 위의 부모 로그인 계정에서 엄마 비밀번호를 설정합니다.</span></li><li><strong>공개 HTTPS 주소 전달</strong><span>외부 접속 설정이 끝난 뒤 대시보드 주소만 전달합니다. Tailscale 앱은 필요하지 않습니다.</span></li><li><strong>엄마 계정으로 로그인</strong><span>휴대폰 Chrome에서 ‘엄마’를 선택하고 본인 비밀번호로 로그인합니다.</span></li><li><strong>필요할 때만 관리자 PIN</strong><span>일정 수정 등 관리 작업은 기존 관리자 PIN으로 한 번 더 확인합니다.</span></li></ol><p className="security-note">현재 로그인 기능 준비와 인터넷 공개는 별개입니다. HTTPS와 공격 차단 검증 전에는 공유기 포트를 열지 마세요.</p></section>}

type FinanceTransaction={id:string;amount:number;category:string;payer:string;occurredOn:string;memo:string;version:number};
type FinanceAccount={id:string;kind:"loan"|"installment_savings";name:string;balanceAmount:number;interestRate:number;monthlyAmount:number;paymentDay:number;startedOn:string;maturityOn:string;notes:string;version:number};
type FinanceOverview={settings:{salaryAmount:number;budgetStartDay:number;version:number};period:{from:string;to:string;offset:number};transactions:FinanceTransaction[];accounts:FinanceAccount[]};
const financeCategories:Record<string,string>={food:"식비",living:"생활",transport:"교통",education:"교육",medical:"의료",leisure:"여가",utilities:"공과금",loan_payment:"대출 상환",savings:"적금 납입",other:"기타"};
const localToday=()=>new Date().toLocaleDateString("sv-SE");
const emptyExpense=(occurredOn=localToday())=>({id:"",amount:"",category:"food",payer:"family",occurredOn,memo:"",version:0});
const emptyAccount=()=>({id:"",kind:"loan" as FinanceAccount["kind"],name:"",balanceAmount:"",interestRate:"",monthlyAmount:"",paymentDay:"21",startedOn:"",maturityOn:"",notes:"",version:0});

function financePrivate(auth:DeviceAuth|null){return auth&&auth.device.type!=="trusted_pc"&&auth.device.type!=="parent_mobile"}
function financeHeaders(){return {"Content-Type":"application/json","X-CSRF-Token":readCookie("family_dashboard_csrf")}}
async function financeMessage(response:Response,fallback:string){try{return (await response.json() as {message?:string}).message||fallback}catch{return fallback}}
function expenseDateForPeriod(overview:FinanceOverview|null){if(!overview||overview.period.offset===0)return localToday();return overview.period.from}
function nextPaymentDate(paymentDay:number,from=new Date()){
  const candidate=(year:number,month:number)=>new Date(year,month,Math.min(paymentDay,new Date(year,month+1,0).getDate()),12);
  let due=candidate(from.getFullYear(),from.getMonth());if(due<new Date(from.getFullYear(),from.getMonth(),from.getDate(),0))due=candidate(from.getFullYear(),from.getMonth()+1);return due;
}
function daysUntil(value:Date){const today=new Date();const start=new Date(today.getFullYear(),today.getMonth(),today.getDate()).getTime();return Math.ceil((value.getTime()-start)/86400000)}

function LedgerDashboard({auth}:{auth:DeviceAuth|null}){
  const [overview,setOverview]=useState<FinanceOverview|null>(null);const [salary,setSalary]=useState("");const [expense,setExpense]=useState(emptyExpense);const [open,setOpen]=useState(false);const [error,setError]=useState("");const [periodOffset,setPeriodOffset]=useState(0);const [categoryFilter,setCategoryFilter]=useState("all");const [payerFilter,setPayerFilter]=useState("all");const [search,setSearch]=useState("");
  const load=useCallback(async()=>{if(!auth||financePrivate(auth))return;const response=await fetch(`/api/v1/finance?offset=${periodOffset}`,{cache:"no-store"});if(response.ok){const data=await response.json() as FinanceOverview;setOverview(data);setSalary(String(data.settings.salaryAmount||""));setError("");}else setError(await financeMessage(response,"가계부를 불러오지 못했습니다."));},[auth,periodOffset]);
  useEffect(()=>{void load()},[load]);
  if(financePrivate(auth))return <FinancePrivate/>;
  const transactions=overview?.transactions||[];const total=transactions.reduce((sum,item)=>sum+item.amount,0);const budget=overview?.settings.salaryAmount||0;const remaining=budget-total;const usage=budget?Math.round(total/budget*100):0;const normalizedSearch=search.trim().toLowerCase();
  const filteredTransactions=transactions.filter(item=>(categoryFilter==="all"||item.category===categoryFilter)&&(payerFilter==="all"||item.payer===payerFilter)&&(!normalizedSearch||`${financeCategories[item.category]||"기타"} ${item.memo||""}`.toLowerCase().includes(normalizedSearch)));
  const categorySummary=Object.entries(transactions.reduce<Record<string,number>>((summary,item)=>{summary[item.category]=(summary[item.category]||0)+item.amount;return summary},{})).sort((left,right)=>right[1]-left[1]);
  const monthlyLoan=(overview?.accounts||[]).filter(item=>item.kind==="loan").reduce((sum,item)=>sum+item.monthlyAmount,0);const monthlySavings=(overview?.accounts||[]).filter(item=>item.kind==="installment_savings").reduce((sum,item)=>sum+item.monthlyAmount,0);
  function changePeriod(next:number){setPeriodOffset(next);setOpen(false);setExpense(emptyExpense());setCategoryFilter("all");setPayerFilter("all");setSearch("")}
  async function saveSalary(event:FormEvent){event.preventDefault();if(!overview)return;const response=await fetch("/api/v1/finance/settings",{method:"PUT",headers:financeHeaders(),body:JSON.stringify({salaryAmount:Number(salary)||0,budgetStartDay:21,version:overview.settings.version})});if(response.ok)void load();else setError(await financeMessage(response,"월급 예산을 저장하지 못했습니다."));}
  async function saveExpense(event:FormEvent){event.preventDefault();const response=await fetch(expense.id?`/api/v1/finance/transactions/${expense.id}`:"/api/v1/finance/transactions",{method:expense.id?"PUT":"POST",headers:financeHeaders(),body:JSON.stringify({...expense,amount:Number(expense.amount),version:expense.version})});if(response.ok){setExpense(emptyExpense(expenseDateForPeriod(overview)));setOpen(false);void load()}else setError(await financeMessage(response,"지출을 저장하지 못했습니다."));}
  async function removeExpense(item:FinanceTransaction){if(!window.confirm("이 지출 내역을 삭제할까요?"))return;const response=await fetch(`/api/v1/finance/transactions/${item.id}`,{method:"DELETE",headers:financeHeaders(),body:JSON.stringify({version:item.version})});if(response.ok)void load();else setError(await financeMessage(response,"지출을 삭제하지 못했습니다."));}
  return <section className="ledger-placeholder finance-dashboard tab-content"><div className="ledger-icon" aria-hidden="true">₩</div><p className="label">21일 월급 가계부</p><h2>{overview?`${formatFinanceDate(overview.period.from)}부터 ${formatFinanceDate(overview.period.to)}까지`:"가계부 불러오는 중"}</h2><p>월급이 들어오는 21일부터 다음 달 20일까지를 하나의 생활비 기간으로 계산합니다.</p>
    <div className="period-navigation" aria-label="가계부 기간 이동"><button className="secondary" type="button" disabled={periodOffset<=-120} onClick={()=>changePeriod(periodOffset-1)}>← 이전</button>{periodOffset!==0&&<button className="secondary" type="button" onClick={()=>changePeriod(0)}>현재 기간</button>}<span>{periodOffset===0?"현재":periodOffset<0?`${Math.abs(periodOffset)}개월 전`:`${periodOffset}개월 후`}</span><button className="secondary" type="button" disabled={periodOffset>=12} onClick={()=>changePeriod(periodOffset+1)}>다음 →</button></div>
    <div className="ledger-preview"><article><span>월급 예산</span><strong>{budget?formatWon(budget):"설정 전"}</strong></article><article><span>기록된 지출</span><strong>{formatWon(total)}</strong></article><article className={remaining<0?"budget-over":""}><span>남은 예산</span><strong>{budget?formatWon(remaining):"—"}</strong></article></div>
    {budget>0&&<section className={`budget-progress ${usage>100?"over":""}`}><div><strong>예산 사용률 {usage}%</strong><span>{formatWon(total)} / {formatWon(budget)}</span></div><progress max={Math.max(budget,total)} value={total} aria-label={`예산 사용률 ${usage}%`}/><small>대출 월 {formatWon(monthlyLoan)} · 적금 월 {formatWon(monthlySavings)}은 아래 지출 합계와 별도로 등록된 참고 금액입니다.</small></section>}
    {auth?.permissions.admin?<form className="salary-budget-form" onSubmit={saveSalary}><label><span>한 달 월급 예산</span><input inputMode="numeric" min="0" step="1000" type="number" value={salary} onChange={event=>setSalary(event.target.value)}/></label><button type="submit">월급 예산 저장</button></form>:<p className="security-note">월급 예산과 지출을 변경하려면 설정 탭에서 관리자 모드를 여세요.</p>}
    {categorySummary.length>0&&<section className="category-summary"><div className="finance-section-heading"><div><p className="label">카테고리별 지출</p><strong>{categorySummary.length}개 분류</strong></div></div><div>{categorySummary.map(([category,amount])=><article key={category}><span>{financeCategories[category]||"기타"}</span><div><i style={{width:`${Math.max(4,total?amount/total*100:0)}%`}}/></div><strong>{formatWon(amount)} <small>{total?Math.round(amount/total*100):0}%</small></strong></article>)}</div></section>}
    <div className="finance-section-heading"><div><p className="label">선택 기간 지출</p><strong>{transactions.length}건</strong></div>{auth?.permissions.admin&&<button onClick={()=>{setExpense(emptyExpense(expenseDateForPeriod(overview)));setOpen(!open)}}>{open&&!expense.id?"입력 닫기":"지출 추가"}</button>}</div>
    {open&&auth?.permissions.admin&&<form className="finance-form" onSubmit={saveExpense}><div className="form-grid"><label><span>금액</span><input type="number" min="1" step="100" required value={expense.amount} onChange={e=>setExpense({...expense,amount:e.target.value})}/></label><label><span>날짜</span><input type="date" required value={expense.occurredOn} onChange={e=>setExpense({...expense,occurredOn:e.target.value})}/></label><label><span>분류</span><select value={expense.category} onChange={e=>setExpense({...expense,category:e.target.value})}>{Object.entries(financeCategories).map(([value,label])=><option value={value} key={value}>{label}</option>)}</select></label><label><span>결제자</span><select value={expense.payer} onChange={e=>setExpense({...expense,payer:e.target.value})}><option value="family">가족 공통</option><option value="dad">아빠</option><option value="mom">엄마</option></select></label></div><label><span>메모</span><input maxLength={500} value={expense.memo} onChange={e=>setExpense({...expense,memo:e.target.value})}/></label><div className="button-row"><button type="submit">{expense.id?"수정 저장":"지출 저장"}</button><button type="button" className="secondary" onClick={()=>{setOpen(false);setExpense(emptyExpense())}}>취소</button></div></form>}
    {transactions.length>0&&<div className="finance-filters"><label><span>검색</span><input type="search" placeholder="메모 또는 분류" value={search} onChange={event=>setSearch(event.target.value)}/></label><label><span>분류</span><select value={categoryFilter} onChange={event=>setCategoryFilter(event.target.value)}><option value="all">전체 분류</option>{Object.entries(financeCategories).map(([value,label])=><option value={value} key={value}>{label}</option>)}</select></label><label><span>결제자</span><select value={payerFilter} onChange={event=>setPayerFilter(event.target.value)}><option value="all">전체 결제자</option><option value="family">가족 공통</option><option value="dad">아빠</option><option value="mom">엄마</option></select></label><strong>{filteredTransactions.length}건 표시</strong></div>}
    {error&&<p className="form-error">{error}</p>}<div className="finance-list">{filteredTransactions.map(item=><article key={item.id}><div><strong>{financeCategories[item.category]||"기타"} · {formatWon(item.amount)}</strong><span>{item.occurredOn} · {item.payer==="dad"?"아빠":item.payer==="mom"?"엄마":"가족 공통"}{item.memo?` · ${item.memo}`:""}</span></div>{auth?.permissions.admin&&<div className="button-row"><button className="secondary" onClick={()=>{setExpense({id:item.id,amount:String(item.amount),category:item.category,payer:item.payer,occurredOn:item.occurredOn,memo:item.memo||"",version:item.version});setOpen(true)}}>수정</button><button className="danger" onClick={()=>void removeExpense(item)}>삭제</button></div>}</article>)}{overview&&transactions.length===0&&<p className="schedule-empty">이 기간에 입력한 지출이 없습니다.</p>}{transactions.length>0&&filteredTransactions.length===0&&<p className="schedule-empty">조건에 맞는 지출이 없습니다.</p>}</div>
    <p className="finance-note">카카오톡·문자·은행 비밀번호는 수집하지 않습니다. 입력한 월급과 지출은 가족 NAS에 저장되며 부모 기기에서만 조회됩니다.</p></section>
}

function InvestmentDashboard({auth}:{auth:DeviceAuth|null}){
  const [overview,setOverview]=useState<FinanceOverview|null>(null);const [form,setForm]=useState(emptyAccount);const [open,setOpen]=useState(false);const [error,setError]=useState("");
  const load=useCallback(async()=>{if(!auth||financePrivate(auth))return;const response=await fetch("/api/v1/finance",{cache:"no-store"});if(response.ok){setOverview(await response.json() as FinanceOverview);setError("")}else setError(await financeMessage(response,"대출·적금 정보를 불러오지 못했습니다."));},[auth]);useEffect(()=>{void load()},[load]);
  if(financePrivate(auth))return <FinancePrivate/>;
  const loans=overview?.accounts.filter(item=>item.kind==="loan")||[];const savings=overview?.accounts.filter(item=>item.kind==="installment_savings")||[];const loanBalance=loans.reduce((sum,item)=>sum+item.balanceAmount,0);const monthlyLoan=loans.reduce((sum,item)=>sum+item.monthlyAmount,0);const monthlySavings=savings.reduce((sum,item)=>sum+item.monthlyAmount,0);const annualLoanInterest=loans.reduce((sum,item)=>sum+item.balanceAmount*item.interestRate/100,0);const upcoming=(overview?.accounts||[]).map(item=>({item,due:nextPaymentDate(item.paymentDay)})).sort((left,right)=>left.due.getTime()-right.due.getTime());
  async function save(event:FormEvent){event.preventDefault();const response=await fetch(form.id?`/api/v1/finance/accounts/${form.id}`:"/api/v1/finance/accounts",{method:form.id?"PUT":"POST",headers:financeHeaders(),body:JSON.stringify({...form,balanceAmount:Number(form.balanceAmount)||0,interestRate:Number(form.interestRate)||0,monthlyAmount:Number(form.monthlyAmount)||0,paymentDay:Number(form.paymentDay)||21})});if(response.ok){setForm(emptyAccount());setOpen(false);void load()}else setError(await financeMessage(response,"대출·적금 정보를 저장하지 못했습니다."));}
  async function remove(item:FinanceAccount){if(!window.confirm(`‘${item.name}’ 항목을 삭제할까요?`))return;const response=await fetch(`/api/v1/finance/accounts/${item.id}`,{method:"DELETE",headers:financeHeaders(),body:JSON.stringify({version:item.version})});if(response.ok)void load();else setError(await financeMessage(response,"항목을 삭제하지 못했습니다."));}
  function edit(item:FinanceAccount){setForm({id:item.id,kind:item.kind,name:item.name,balanceAmount:String(item.balanceAmount),interestRate:String(item.interestRate),monthlyAmount:String(item.monthlyAmount),paymentDay:String(item.paymentDay),startedOn:item.startedOn||"",maturityOn:item.maturityOn||"",notes:item.notes||"",version:item.version});setOpen(true)}
  return <section className="investment-placeholder finance-dashboard tab-content"><div className="investment-heading"><div className="investment-icon" aria-hidden="true">↗</div><div><p className="label">대출·적금</p><h2>생활비 밖의 금융 현황</h2><p>대출 원금과 이자 부담, 적금 납입액과 만기일을 함께 관리합니다.</p></div></div><div className="investment-summary"><article><span>남은 대출 원금</span><strong>{formatWon(loanBalance)}</strong><small>{loans.length}개 대출</small></article><article><span>월 대출 상환액</span><strong>{formatWon(monthlyLoan)}</strong><small>원금·이자 상환 입력값</small></article><article><span>월 적금 납입액</span><strong>{formatWon(monthlySavings)}</strong><small>{savings.length}개 적금</small></article><article><span>월 고정 금융부담</span><strong>{formatWon(monthlyLoan+monthlySavings)}</strong><small>대출 상환 + 적금 납입</small></article></div>
    {upcoming.length>0&&<section className="payment-calendar"><div className="finance-section-heading"><div><p className="label">다가오는 납부 일정</p><strong>월 고정 {formatWon(monthlyLoan+monthlySavings)}</strong></div></div><div>{upcoming.map(({item,due})=><article key={item.id}><time dateTime={due.toLocaleDateString("sv-SE")}><strong>{due.toLocaleDateString("ko-KR",{month:"short",day:"numeric"})}</strong><span>{daysUntil(due)===0?"오늘":`D-${daysUntil(due)}`}</span></time><div><span className="badge">{item.kind==="loan"?"대출":"적금"}</span><strong>{item.name}</strong><small>{item.kind==="loan"?"상환":"납입"} 예정 {formatWon(item.monthlyAmount)}{item.maturityOn?` · 만기 ${item.maturityOn}`:""}</small></div></article>)}</div></section>}
    <div className="finance-section-heading"><div><p className="label">등록 항목</p><strong>{overview?.accounts.length||0}개</strong></div>{auth?.permissions.admin&&<button onClick={()=>{setForm(emptyAccount());setOpen(!open)}}>{open&&!form.id?"입력 닫기":"대출·적금 추가"}</button>}</div>{!auth?.permissions.admin&&<p className="security-note">대출·적금을 변경하려면 설정 탭에서 관리자 모드를 여세요.</p>}
    {open&&auth?.permissions.admin&&<form className="finance-form" onSubmit={save}><div className="form-grid"><label><span>종류</span><select value={form.kind} onChange={e=>setForm({...form,kind:e.target.value as FinanceAccount["kind"]})}><option value="loan">대출</option><option value="installment_savings">적금</option></select></label><label><span>이름</span><input maxLength={100} required placeholder={form.kind==="loan"?"예: 주택담보대출":"예: 가족 여행 적금"} value={form.name} onChange={e=>setForm({...form,name:e.target.value})}/></label><label><span>{form.kind==="loan"?"남은 원금":"현재 적립액"}</span><input type="number" min="0" step="1000" required value={form.balanceAmount} onChange={e=>setForm({...form,balanceAmount:e.target.value})}/></label><label><span>연 금리 (%)</span><input type="number" min="0" max="1000" step="0.01" value={form.interestRate} onChange={e=>setForm({...form,interestRate:e.target.value})}/></label><label><span>{form.kind==="loan"?"월 상환액":"월 납입액"}</span><input type="number" min="0" step="1000" value={form.monthlyAmount} onChange={e=>setForm({...form,monthlyAmount:e.target.value})}/></label><label><span>납부일</span><input type="number" min="1" max="31" value={form.paymentDay} onChange={e=>setForm({...form,paymentDay:e.target.value})}/></label><label><span>시작일</span><input type="date" value={form.startedOn} onChange={e=>setForm({...form,startedOn:e.target.value})}/></label><label><span>만기일</span><input type="date" value={form.maturityOn} onChange={e=>setForm({...form,maturityOn:e.target.value})}/></label></div><label><span>메모</span><textarea maxLength={1000} value={form.notes} onChange={e=>setForm({...form,notes:e.target.value})}/></label><div className="button-row"><button type="submit">{form.id?"수정 저장":"항목 저장"}</button><button type="button" className="secondary" onClick={()=>{setOpen(false);setForm(emptyAccount())}}>취소</button></div></form>}
    {error&&<p className="form-error">{error}</p>}<div className="finance-account-list">{overview?.accounts.map(item=><article key={item.id} className={item.kind}><div><span className="badge">{item.kind==="loan"?"대출":"적금"}</span><strong>{item.name}</strong><p>{item.kind==="loan"?`남은 원금 ${formatWon(item.balanceAmount)} · 월 상환 ${formatWon(item.monthlyAmount)}`:`현재 적립 ${formatWon(item.balanceAmount)} · 월 납입 ${formatWon(item.monthlyAmount)}`}</p><small>연 {item.interestRate.toFixed(2)}% · 매월 {item.paymentDay}일{item.maturityOn?` · 만기 ${item.maturityOn}`:""}{item.kind==="loan"&&item.balanceAmount?` · 월 예상 이자 약 ${formatWon(Math.round(item.balanceAmount*item.interestRate/100/12))}`:""}</small>{item.notes&&<p>{item.notes}</p>}</div>{auth?.permissions.admin&&<div className="button-row"><button className="secondary" onClick={()=>edit(item)}>수정</button><button className="danger" onClick={()=>void remove(item)}>삭제</button></div>}</article>)}{overview&&overview.accounts.length===0&&<p className="schedule-empty">등록된 대출이나 적금이 없습니다.</p>}</div><p className="finance-note">현재 원금 기준 단순 연 이자는 약 {formatWon(Math.round(annualLoanInterest))}입니다. 월 예상 이자는 원금×연 금리÷12의 참고값이며 실제 금융기관 청구액과 다를 수 있습니다.</p></section>
}

function FinancePrivate(){return <section className="investment-placeholder tab-content"><div className="investment-heading"><div className="investment-icon" aria-hidden="true">⌁</div><div><p className="label">부모 전용</p><h2>이 기기에서는 금융정보를 표시하지 않습니다</h2><p>월급, 지출, 대출과 적금은 등록된 부모 모바일 또는 관리자 PC에서만 볼 수 있습니다.</p></div></div></section>}
function formatFinanceDate(value:string){return new Date(`${value}T12:00:00`).toLocaleDateString("ko-KR",{month:"long",day:"numeric"})}
function formatWon(value:number){return `${new Intl.NumberFormat("ko-KR").format(value)}원`}

type FamilyMember = {
  id: string;
  displayName: string;
  role: string;
};

type ScheduleOccurrence = {
  id?: string;
  title: string;
  locationName?: string;
  placeId?: string;
  notes?: string;
  visibility?: string;
  tag?: string;
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

type ScheduleTrashEntry = {
  schedule: ScheduleOccurrence;
  deletedAt: string;
  restoreUntil: string;
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
  placeId: string;
  visibility: string;
  tag: string;
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
  notificationEnabled: boolean;
  timedLeadMinutes: number;
  allDayHour: number;
  timedLeadOptions: number[];
  allDayHours: number[];
  notificationRecipients: string[];
};

function emptyScheduleForm(): ScheduleForm {
  const start = new Date();
  start.setMinutes(Math.ceil(start.getMinutes() / 30) * 30, 0, 0);
  const end = new Date(start.getTime() + 60 * 60 * 1000);
	const today = toLocalInput(start).slice(0, 10);
  return {
    id: "", title: "", locationName: "", placeId: "", visibility: "family", tag: "general",
    startsAt: toLocalInput(start), endsAt: toLocalInput(end),
    participants: [], version: 0, weeklyRepeat: false,
    weekdays: [start.getDay()], recurrenceEndsOn: "",
    occurrenceKey: "", occurrenceVersion: 0,
    allDay: false, startDate: today, endDate: today,
    notificationEnabled: true, timedLeadMinutes: 30, allDayHour: 20,
    timedLeadOptions: [30], allDayHours: [20], notificationRecipients: ["dad","mom"],
  };
}

const scheduleTagLabels: Record<string,string> = { general: "일반", academy: "학원", after_school: "방과후" };

function dateKey(value: Date): string {
  const year = value.getFullYear();
  const month = String(value.getMonth() + 1).padStart(2, "0");
  const day = String(value.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function calendarRange(month: Date): { days: Date[]; from: Date; to: Date } {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const from = new Date(first);
  from.setDate(from.getDate() - from.getDay());
  const last = new Date(month.getFullYear(), month.getMonth() + 1, 0);
  const to = new Date(last);
  to.setDate(to.getDate() + (6 - to.getDay()) + 1);
  const days: Date[] = [];
  for (const cursor = new Date(from); cursor < to; cursor.setDate(cursor.getDate() + 1)) {
    days.push(new Date(cursor));
  }
  return { days, from, to };
}

function weeklyRange(target: Date): Date[] {
  const monday = new Date(target.getFullYear(), target.getMonth(), target.getDate());
  monday.setDate(monday.getDate() - ((monday.getDay() + 6) % 7));
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(monday);
    day.setDate(monday.getDate() + index);
    return day;
  });
}

function occurrenceIncludesDate(item: ScheduleOccurrence, target: string): boolean {
  if (item.timeKind === "all_day") {
    return Boolean(item.startDate && item.endDate && item.startDate <= target && item.endDate >= target);
  }
  if (!item.startsAt || !item.endsAt) return false;
  const dayStart = new Date(`${target}T00:00:00`);
  const dayEnd = new Date(dayStart);
  dayEnd.setDate(dayEnd.getDate() + 1);
  return new Date(item.startsAt) < dayEnd && new Date(item.endsAt) > dayStart;
}

function scheduleFormForDate(target: string, participants: string[]): ScheduleForm {
  const next = emptyScheduleForm();
  const startTime = next.startsAt.slice(11);
  const endTime = next.endsAt.slice(11);
  return {
    ...next,
    startsAt: `${target}T${startTime}`,
    endsAt: `${target}T${endTime}`,
    startDate: target,
    endDate: target,
    participants,
    weekdays: [new Date(`${target}T12:00:00`).getDay()],
  };
}

function ScheduleBoard({ auth, focusMemberId, weekly = false }: { auth: DeviceAuth | null; focusMemberId?: string; weekly?: boolean }) {
  const [members, setMembers] = useState<FamilyMember[]>([]);
  const [occurrences, setOccurrences] = useState<ScheduleOccurrence[]>([]);
  const [familyStatuses, setFamilyStatuses] = useState<FamilyStatusItem[]>([]);
  const [selectedMember, setSelectedMember] = useState(focusMemberId||"");
  const [form, setForm] = useState<ScheduleForm>(emptyScheduleForm);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [trash, setTrash] = useState<ScheduleTrashEntry[]>([]);
  const [showTrash, setShowTrash] = useState(false);
  const [places, setPlaces] = useState<Array<{id:string;name:string;regionLabel:string}>>([]);
  const [calendarMonth, setCalendarMonth] = useState(() => {
    const today = new Date();
    return new Date(today.getFullYear(), today.getMonth(), 1);
  });
  const [selectedDate, setSelectedDate] = useState(() => dateKey(new Date()));
  const [selectedTag, setSelectedTag] = useState("all");

  const refreshSchedules = useCallback(async () => {
    if (!auth) {
      setMembers([]);
      setOccurrences([]);
      setFamilyStatuses([]);
      return;
    }
    const { from, to } = calendarRange(calendarMonth);
    try {
      const [memberResponse, scheduleResponse, statusResponse, placeResponse] = await Promise.all([
        fetch("/api/v1/family-members", { cache: "no-store" }),
        fetch(`/api/v1/schedule-occurrences?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`, { cache: "no-store" }),
        fetch("/api/v1/family-status", { cache: "no-store" }),
        fetch("/api/v1/places", { cache: "no-store" }),
      ]);
      if (!memberResponse.ok || !scheduleResponse.ok || !statusResponse.ok) {
        throw new Error("일정 캘린더와 가족 상태를 불러오지 못했습니다.");
      }
      const memberResult = await memberResponse.json() as { members: FamilyMember[] };
      const scheduleResult = await scheduleResponse.json() as { occurrences: ScheduleOccurrence[] };
      const statusResult = await statusResponse.json() as { members: FamilyStatusItem[] };
      setMembers(memberResult.members);
      setOccurrences(scheduleResult.occurrences);
      setFamilyStatuses(statusResult.members);
      if (placeResponse.ok) setPlaces((await placeResponse.json() as {places:Array<{id:string;name:string;regionLabel:string}>}).places ?? []);
      setError("");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "일정 캘린더와 가족 상태를 불러오지 못했습니다.");
    }
  }, [auth, calendarMonth]);

  const refreshTrash = useCallback(async () => {
    if (!auth?.permissions.admin) {
      setTrash([]);
      setShowTrash(false);
      return;
    }
    const response = await fetch("/api/admin/schedule-trash", { cache: "no-store" });
    if (!response.ok) return;
    const result = await response.json() as { schedules: ScheduleTrashEntry[] };
    setTrash(result.schedules);
  }, [auth]);

  useEffect(() => {
    void refreshSchedules();
    const timer = window.setInterval(() => void refreshSchedules(), 60_000);
    return () => window.clearInterval(timer);
  }, [refreshSchedules]);

  useEffect(()=>setSelectedMember(focusMemberId||""),[focusMemberId]);

  useEffect(() => {
    void refreshTrash();
  }, [refreshTrash]);

  if (!auth) return null;

  const effectiveMember=focusMemberId||selectedMember;
  const filteredOccurrences = effectiveMember
    ? occurrences.filter((item) => item.participants?.includes(effectiveMember))
    : occurrences;
  const taggedOccurrences = selectedTag === "all"
    ? filteredOccurrences
    : filteredOccurrences.filter((item) => (item.tag ?? "general") === selectedTag);
  const visibleFamilyStatuses=focusMemberId?familyStatuses.filter(item=>item.member.id===focusMemberId):familyStatuses;
  const visibleScheduleTrash=focusMemberId?trash.filter(entry=>entry.schedule.participants?.includes(focusMemberId)):trash;
  const calendarDays = calendarRange(calendarMonth).days;
  const weeklyDays = weeklyRange(new Date(`${selectedDate}T12:00:00`));
  const selectedOccurrences = taggedOccurrences
    .filter((item) => occurrenceIncludesDate(item, selectedDate))
    .sort((left, right) => {
      if (left.timeKind === "all_day" && right.timeKind !== "all_day") return -1;
      if (left.timeKind !== "all_day" && right.timeKind === "all_day") return 1;
      return (left.startsAt ?? "").localeCompare(right.startsAt ?? "");
    });
  const todayKey = dateKey(new Date());

  function startNewSchedule(target = selectedDate) {
    setSelectedDate(target);
    const next = scheduleFormForDate(target, focusMemberId ? [focusMemberId] : []);
    setForm(weekly ? { ...next, tag: "academy", weeklyRepeat: true } : next);
    setShowForm(true);
  }

  function moveCalendarMonth(offset: number) {
    setCalendarMonth((current) => new Date(current.getFullYear(), current.getMonth() + offset, 1));
  }

  function showCurrentMonth() {
    const today = new Date();
    setCalendarMonth(new Date(today.getFullYear(), today.getMonth(), 1));
    setSelectedDate(dateKey(today));
  }

  function moveCalendarWeek(offset: number) {
    const next = new Date(`${selectedDate}T12:00:00`);
    next.setDate(next.getDate() + (offset * 7));
    setSelectedDate(dateKey(next));
    setCalendarMonth(new Date(next.getFullYear(), next.getMonth(), 1));
  }

  function showCurrentWeek() {
    const today = new Date();
    setCalendarMonth(new Date(today.getFullYear(), today.getMonth(), 1));
    setSelectedDate(dateKey(today));
  }

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
      placeId: item.placeId ?? "",
      visibility: item.visibility ?? "family",
      tag: item.tag ?? "general",
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
      notificationEnabled: true,
      timedLeadMinutes: 30,
      allDayHour: 20,
      timedLeadOptions: [30],
      allDayHours: [20],
      notificationRecipients: ["dad","mom"],
    });
    setShowForm(true);
    void fetch(`/api/v1/schedules/${encodeURIComponent(item.id)}/notification`, { cache: "no-store" })
      .then(async (response) => response.ok ? await response.json() as {
        enabled: boolean; timedLeadMinutes: number; allDayHour: number; timedLeadOptions?:number[]; allDayHours?:number[]; recipients?:string[];
      } : null)
      .then((setting) => {
        if (!setting) return;
        setForm((current) => current.id === item.id ? {
          ...current,
          notificationEnabled: setting.enabled,
          timedLeadMinutes: setting.timedLeadMinutes,
          allDayHour: setting.allDayHour,
          timedLeadOptions: setting.timedLeadOptions ?? [setting.timedLeadMinutes],
          allDayHours: setting.allDayHours ?? [setting.allDayHour],
          notificationRecipients: setting.recipients ?? ["dad","mom"],
        } : current);
      });
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
          placeId: form.placeId,
          visibility: form.visibility,
          tag: form.tag,
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
      const result = await response.json() as { id?: string; code?: string; message?: string };
      if (response.status === 409 && result.code === "overlap_warning" && !confirmOverlap) {
        if (window.confirm("같은 가족에게 겹치는 일정이 있습니다. 그래도 저장할까요?")) {
          await save(true);
        }
        return;
      }
      if (!response.ok) throw new Error(result.message ?? "일정을 저장하지 못했습니다.");
      const scheduleID = result.id ?? form.id;
      if (scheduleID) {
        const notificationResponse = await fetch(`/api/v1/schedules/${encodeURIComponent(scheduleID)}/notification`, {
          method: "PUT",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": readCookie("family_dashboard_csrf"),
          },
          body: JSON.stringify({
            enabled: form.notificationEnabled,
            timedLeadMinutes: form.timedLeadMinutes,
            allDayHour: form.allDayHour,
            timedLeadOptions: form.timedLeadOptions,
            allDayHours: form.allDayHours,
            recipients: form.notificationRecipients,
          }),
        });
        if (!notificationResponse.ok) throw new Error("일정은 저장됐지만 알림 설정을 저장하지 못했습니다.");
      }
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
    if (!window.confirm("이 반복 일정의 이번 회차만 삭제할까요?")) return;
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
        throw new Error(result.message ?? "이번 회차를 삭제하지 못했습니다.");
      }
      setForm(emptyScheduleForm());
      setShowForm(false);
      await refreshSchedules();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "이번 회차를 삭제하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function deleteWholeSchedule() {
    if (!form.id || form.version < 1) return;
    const message = form.occurrenceKey
      ? "이 반복 일정 전체를 휴지통으로 이동할까요? 모든 회차가 일정 화면에서 사라집니다."
      : "이 일정을 휴지통으로 이동할까요?";
    if (!window.confirm(message)) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/v1/schedules/${encodeURIComponent(form.id)}`, {
        method: "DELETE",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": readCookie("family_dashboard_csrf"),
        },
        body: JSON.stringify({ version: form.version }),
      });
      if (!response.ok) {
        const result = await response.json() as { message?: string };
        throw new Error(result.message ?? "일정을 삭제하지 못했습니다.");
      }
      setForm(emptyScheduleForm());
      setShowForm(false);
      await Promise.all([refreshSchedules(), refreshTrash()]);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "일정을 삭제하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function restoreTrashedSchedule(entry: ScheduleTrashEntry) {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/schedule-trash/${encodeURIComponent(entry.schedule.id!)}/restore`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": readCookie("family_dashboard_csrf"),
        },
        body: JSON.stringify({ version: entry.schedule.version }),
      });
      if (!response.ok) {
        const result = await response.json() as { message?: string };
        throw new Error(result.message ?? "일정을 복원하지 못했습니다.");
      }
      await Promise.all([refreshSchedules(), refreshTrash()]);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "일정을 복원하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function permanentlyDeleteTrashedSchedule(entry: ScheduleTrashEntry) {
    if (!window.confirm(`‘${entry.schedule.title}’ 일정을 영구 삭제할까요? 이 작업은 되돌릴 수 없습니다.`)) return;
    setBusy(true);
    setError("");
    try {
      const response = await fetch(`/api/admin/schedule-trash/${encodeURIComponent(entry.schedule.id!)}`, {
        method: "DELETE",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": readCookie("family_dashboard_csrf"),
        },
        body: JSON.stringify({ version: entry.schedule.version }),
      });
      if (!response.ok) {
        const result = await response.json() as { message?: string };
        throw new Error(result.message ?? "일정을 영구 삭제하지 못했습니다.");
      }
      await refreshTrash();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "일정을 영구 삭제하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="schedule-card">
      <div className="schedule-heading">
        <div>
          <p className="label">{weekly ? "주간 학원 시간표" : "일정 캘린더"}</p>
          <strong>{weekly
            ? `${weeklyDays[0].toLocaleDateString("ko-KR", { month: "long", day: "numeric" })} – ${weeklyDays[6].toLocaleDateString("ko-KR", { month: "long", day: "numeric" })}`
            : calendarMonth.toLocaleDateString("ko-KR", { year: "numeric", month: "long" })}</strong>
        </div>
        {auth.permissions.admin && (
          <button type="button" onClick={() => {
            if (showForm && !form.id) {
              setShowForm(false);
              return;
            }
            startNewSchedule();
          }}>
            {form.id ? "새 일정" : showForm ? "입력 닫기" : "일정 추가"}
          </button>
        )}
      </div>

      <div className="family-status-grid" aria-label="일정 기반 가족 상태">
        {visibleFamilyStatuses.map((item) => (
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

      {!focusMemberId&&<div className="schedule-filters" aria-label="가족별 일정 필터">
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
      </div>}

      <div className="calendar-toolbar" aria-label={weekly ? "시간표 주 이동" : "달력 월 이동"}>
        <button type="button" className="secondary calendar-arrow" aria-label={weekly ? "이전 주" : "이전 달"} onClick={() => weekly ? moveCalendarWeek(-1) : moveCalendarMonth(-1)}>‹</button>
        <button type="button" className="secondary" onClick={weekly ? showCurrentWeek : showCurrentMonth}>{weekly ? "이번 주" : "오늘"}</button>
        <button type="button" className="secondary calendar-arrow" aria-label={weekly ? "다음 주" : "다음 달"} onClick={() => weekly ? moveCalendarWeek(1) : moveCalendarMonth(1)}>›</button>
      </div>

      {weekly && <div className="schedule-tag-filters" aria-label="일정 태그 필터">
        {[["all","전체"],["academy","학원"],["after_school","방과후"],["general","일반"]].map(([value,label])=><button type="button" key={value} className={selectedTag===value?"filter-active":"secondary"} aria-pressed={selectedTag===value} onClick={()=>setSelectedTag(value)}>{label}</button>)}
      </div>}

      {weekly ? <div className="weekly-timetable-scroll"><div className="weekly-timetable" aria-label="딸 주간 학원 시간표">
        {weeklyDays.map(day => {
          const key = dateKey(day);
          const dayOccurrences = taggedOccurrences
            .filter(item => occurrenceIncludesDate(item, key))
            .sort((left, right) => (left.startsAt ?? "").localeCompare(right.startsAt ?? ""));
          return <button type="button" key={key} className={`weekly-timetable-day${selectedDate===key?" selected":""}${todayKey===key?" today":""}`} aria-pressed={selectedDate===key} onClick={()=>setSelectedDate(key)}>
            <span className="weekly-day-heading"><strong>{day.toLocaleDateString("ko-KR",{weekday:"short"})}</strong><small>{day.getMonth()+1}.{day.getDate()}</small></span>
            <span className="weekly-day-lessons">{dayOccurrences.map((item,index)=><span className={`weekly-lesson tag-${item.tag??"general"}${item.summary?" private":""}`} key={`${item.id??item.title}-${item.occurrenceKey??item.startsAt??index}`}><span className="schedule-tag">{scheduleTagLabels[item.tag??"general"]}</span><small>{item.timeKind==="all_day"?"종일":item.startsAt?new Date(item.startsAt).toLocaleTimeString("ko-KR",{hour:"2-digit",minute:"2-digit",hour12:false}):""}</small><strong>{item.title}</strong>{item.locationName&&<em>{item.locationName}</em>}</span>)}{dayOccurrences.length===0&&<span className="weekly-empty">일정 없음</span>}</span>
          </button>;
        })}
      </div></div> : <div className="schedule-calendar" aria-label={`${calendarMonth.getFullYear()}년 ${calendarMonth.getMonth() + 1}월 일정`}>
        {['일', '월', '화', '수', '목', '금', '토'].map((weekday) => (
          <span key={weekday} className="calendar-weekday" aria-hidden="true">{weekday}</span>
        ))}
        {calendarDays.map((day) => {
          const key = dateKey(day);
          const dayOccurrences = taggedOccurrences.filter((item) => occurrenceIncludesDate(item, key));
          const outsideMonth = day.getMonth() !== calendarMonth.getMonth();
          return (
            <button
              type="button"
              key={key}
              className={`calendar-day${selectedDate === key ? " selected" : ""}${key === todayKey ? " today" : ""}${outsideMonth ? " outside" : ""}`}
              aria-label={`${day.toLocaleDateString("ko-KR", { year: "numeric", month: "long", day: "numeric" })}, 일정 ${dayOccurrences.length}개`}
              aria-pressed={selectedDate === key}
              onClick={() => {
                setSelectedDate(key);
                if (outsideMonth) setCalendarMonth(new Date(day.getFullYear(), day.getMonth(), 1));
              }}
            >
              <span className="calendar-date">{day.getDate()}</span>
              <span className="calendar-events">
                {dayOccurrences.slice(0, 3).map((item, index) => (
                  <span className={`calendar-event${item.summary ? " private" : ""}`} key={`${item.id ?? item.title}-${item.occurrenceKey ?? item.startsAt ?? index}`}>
                    <small>{item.timeKind === "all_day" ? "종일" : item.startsAt ? new Date(item.startsAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit", hour12: false }) : ""}</small>
                    <strong>{item.title}</strong>
                  </span>
                ))}
                {dayOccurrences.length > 3 && <span className="calendar-more">+{dayOccurrences.length - 3}개</span>}
              </span>
            </button>
          );
        })}
      </div>}

      <div className="selected-day-heading">
        <div>
          <p className="label">선택한 날짜</p>
          <strong>{new Date(`${selectedDate}T12:00:00`).toLocaleDateString("ko-KR", { month: "long", day: "numeric", weekday: "short" })}</strong>
        </div>
        {auth.permissions.admin && <button type="button" className="secondary" onClick={() => startNewSchedule(selectedDate)}>이 날짜에 추가</button>}
      </div>

      {showForm && !form.id && auth.permissions.admin && (
        <ScheduleEditor
          form={form}
          members={members}
          places={places}
          busy={busy}
          onChange={setForm}
          onToggleParticipant={toggleParticipant}
          onSave={() => void save()}
          onCancel={() => {
            setForm(scheduleFormForDate(selectedDate, focusMemberId ? [focusMemberId] : []));
            setShowForm(false);
          }}
        />
      )}

      {error && <p className="form-error">{error}</p>}
      <div className="schedule-list">
        {selectedOccurrences.map((item, index) => {
          const key = item.occurrenceKey ? `${item.id}-${item.occurrenceKey}` : item.id ?? `${item.startsAt}-${index}`;
          if (item.id && item.id === form.id &&
            (form.occurrenceKey ? item.occurrenceKey === form.occurrenceKey : !item.occurrenceKey) &&
            auth.permissions.admin) {
            return (
              <article key={key} className="schedule-editor-row">
                <ScheduleEditor
                  form={form}
                  members={members}
                  places={places}
                  busy={busy}
                  onChange={setForm}
                  onToggleParticipant={toggleParticipant}
                  onSave={() => void save()}
                  onCancelOccurrence={form.occurrenceKey ? () => void cancelOccurrence() : undefined}
                  onDeleteSchedule={() => void deleteWholeSchedule()}
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
                <span className={`schedule-tag tag-${item.tag??"general"}`}>{scheduleTagLabels[item.tag??"general"]}</span>
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
        {selectedOccurrences.length === 0 && (
          <p className="schedule-empty">
            {effectiveMember ? "선택한 가족의 이 날짜 일정이 없습니다." : "이 날짜에 등록된 일정이 없습니다."}
          </p>
        )}
      </div>
      {auth.permissions.admin && (
        <div className="schedule-trash">
          <div className="management-heading">
            <div>
              <p className="label">일정 휴지통</p>
              <strong>{visibleScheduleTrash.length === 0 ? "비어 있음" : `${visibleScheduleTrash.length}개 일정`}</strong>
            </div>
            <button type="button" className="secondary" onClick={() => setShowTrash((current) => !current)}>
              {showTrash ? "휴지통 닫기" : "휴지통 보기"}
            </button>
          </div>
          {showTrash && (
            <div className="trash-list">
              {visibleScheduleTrash.map((entry) => (
                <article key={entry.schedule.id}>
                  <div>
                    <strong>{entry.schedule.title}</strong>
                    <span>삭제 {new Date(entry.deletedAt).toLocaleString("ko-KR")}</span>
                    <span>복원 가능 {new Date(entry.restoreUntil).toLocaleDateString("ko-KR")}까지</span>
                  </div>
                  <div className="button-row">
                    <button type="button" className="secondary" disabled={busy} onClick={() => void restoreTrashedSchedule(entry)}>복원</button>
                    <button type="button" className="danger" disabled={busy} onClick={() => void permanentlyDeleteTrashedSchedule(entry)}>영구 삭제</button>
                  </div>
                </article>
              ))}
              {visibleScheduleTrash.length === 0 && <p className="schedule-empty">휴지통에 일정이 없습니다.</p>}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function ScheduleEditor({
  form,
  members,
  places,
  busy,
  onChange,
  onToggleParticipant,
  onSave,
  onCancel,
  onCancelOccurrence,
  onDeleteSchedule,
}: {
  form: ScheduleForm;
  members: FamilyMember[];
  places: Array<{id:string;name:string;regionLabel:string}>;
  busy: boolean;
  onChange: (form: ScheduleForm) => void;
  onToggleParticipant: (id: string) => void;
  onSave: () => void;
  onCancel: () => void;
  onCancelOccurrence?: () => void;
  onDeleteSchedule?: () => void;
}) {
  return (
    <form className="schedule-form" onSubmit={(event) => {
      event.preventDefault();
      onSave();
    }}>
      <label>
        <span>태그</span>
        <select value={form.tag} disabled={Boolean(form.occurrenceKey)} onChange={(event) => {
          const tag = event.target.value;
          onChange({ ...form, tag, weeklyRepeat: !form.id && (tag === "academy" || tag === "after_school") ? true : form.weeklyRepeat });
        }}>
          <option value="general">일반</option>
          <option value="academy">학원</option>
          <option value="after_school">방과후</option>
        </select>
      </label>
      <label>
        <span>제목</span>
        <input value={form.title} maxLength={200} onChange={(event) => onChange({ ...form, title: event.target.value })} />
      </label>
      <label>
        <span>장소</span>
        <input value={form.locationName} maxLength={200} onChange={(event) => onChange({ ...form, locationName: event.target.value })} />
      </label>
      {places.length > 0 && !form.occurrenceKey && <label><span>등록 장소 연결</span><select value={form.placeId} onChange={(event) => { const place=places.find(item=>item.id===event.target.value); onChange({...form,placeId:event.target.value,locationName:place?.name||form.locationName}); }}><option value="">연결 안 함</option>{places.map(place=><option key={place.id} value={place.id}>{place.name} · {place.regionLabel}</option>)}</select></label>}
      {!form.id && (
        <label className="recurrence-toggle">
          <input type="checkbox" checked={form.allDay} onChange={(event) => onChange({ ...form, allDay: event.target.checked })} />
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
      {!form.id && (
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
                  min={form.allDay ? form.startDate : form.startsAt.slice(0, 10)}
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
      <fieldset>
        <legend>{form.occurrenceKey ? "전체 반복 알림" : "일정 알림"}</legend>
        <label className="recurrence-toggle">
          <input
            type="checkbox"
            checked={form.notificationEnabled}
            onChange={(event) => onChange({ ...form, notificationEnabled: event.target.checked })}
          />
          <span>부모 모바일 알림 사용</span>
        </label>
        {form.notificationEnabled && (
          <div className="notification-setting-fields">
            {form.allDay ? (
              <label>
                <span>전날 알림 시각 (복수 선택)</span>
                <div className="participant-options">{[9,12,18,20,21].map(hour=><label key={hour}><input type="checkbox" checked={form.allDayHours.includes(hour)} onChange={()=>onChange({...form,allDayHour:hour,allDayHours:form.allDayHours.includes(hour)?form.allDayHours.filter(value=>value!==hour):[...form.allDayHours,hour]})}/><span>{String(hour).padStart(2,"0")}:00</span></label>)}</div>
              </label>
            ) : (
              <label>
                <span>시작 전 알림 (복수 선택)</span>
                <div className="participant-options">{[[0,"시작 시각"],[10,"10분 전"],[30,"30분 전"],[60,"1시간 전"],[1440,"하루 전"]].map(([lead,label])=><label key={lead}><input type="checkbox" checked={form.timedLeadOptions.includes(lead as number)} onChange={()=>onChange({...form,timedLeadMinutes:lead as number,timedLeadOptions:form.timedLeadOptions.includes(lead as number)?form.timedLeadOptions.filter(value=>value!==lead):[...form.timedLeadOptions,lead as number]})}/><span>{label}</span></label>)}</div>
              </label>
            )}
            <label><span>알림 수신자</span><div className="participant-options">{[["dad","아빠 모바일"],["mom","엄마 모바일"]].map(([owner,label])=><label key={owner}><input type="checkbox" checked={form.notificationRecipients.includes(owner)} onChange={()=>onChange({...form,notificationRecipients:form.notificationRecipients.includes(owner)?form.notificationRecipients.filter(value=>value!==owner):[...form.notificationRecipients,owner]})}/><span>{label}</span></label>)}</div></label>
          </div>
        )}
      </fieldset>
      <div className="button-row">
        <button type="submit" disabled={busy}>{busy ? "저장 중…" : form.occurrenceKey ? "이번 회차 저장" : form.id ? "일정 수정" : "일정 저장"}</button>
        {onCancelOccurrence && (
          <button type="button" className="danger" disabled={busy} onClick={onCancelOccurrence}>이번 회차 삭제</button>
        )}
        {onDeleteSchedule && (
          <button type="button" className="danger" disabled={busy} onClick={onDeleteSchedule}>
            {form.occurrenceKey ? "전체 반복 삭제" : "일정 삭제"}
          </button>
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

type TaskItem = {
  id: string;
  title: string;
  notes?: string;
  priority: "normal" | "important";
  dueKind: "none" | "date" | "datetime";
  dueDate?: string;
  dueMinute?: number;
  assignees: string[];
  repeatKind: "none" | "daily" | "weekly";
  startsOn?: string;
  endsOn?: string;
  weekdays?: number[];
  version: number;
  occurrenceKey: string;
  completedAt?: string;
  overdue: boolean;
};

type TaskTrashEntry = { task: TaskItem; deletedAt: string; restoreUntil: string };

type TaskForm = {
  id: string;
  title: string;
  notes: string;
  priority: "normal" | "important";
  dueKind: "none" | "date" | "datetime";
  dueDate: string;
  dueTime: string;
  assignees: string[];
  repeatKind: "none" | "daily" | "weekly";
  startsOn: string;
  endsOn: string;
  weekdays: number[];
  version: number;
  notificationEnabled: boolean;
  notificationLeadMinutes: number;
  notificationDateHour: number;
  notificationRecipients: string[];
};

function emptyTaskForm(): TaskForm {
  const today = toLocalInput(new Date()).slice(0, 10);
  return {
    id: "", title: "", notes: "", priority: "normal", dueKind: "none",
    dueDate: today, dueTime: "09:00", assignees: [], repeatKind: "none",
    startsOn: today, endsOn: "", weekdays: [new Date().getDay()], version: 0,
    notificationEnabled: false, notificationLeadMinutes: 60, notificationDateHour: 9,
    notificationRecipients: ["dad", "mom"],
  };
}

function TaskBoard({ auth, focusMemberId }: { auth: DeviceAuth | null; focusMemberId?: string }) {
  const [tasks, setTasks] = useState<TaskItem[]>([]);
  const [members, setMembers] = useState<FamilyMember[]>([]);
  const [form, setForm] = useState<TaskForm>(emptyTaskForm);
  const [showForm, setShowForm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [trash, setTrash] = useState<TaskTrashEntry[]>([]);
  const [showTrash, setShowTrash] = useState(false);
  const [history, setHistory] = useState<TaskItem[]>([]);
  const [showHistory, setShowHistory] = useState(false);
  const [skipped, setSkipped] = useState<TaskItem[]>([]);
  const [showSkipped, setShowSkipped] = useState(false);

  const refreshTasks = useCallback(async () => {
    if (!auth) {
      setTasks([]);
      return;
    }
    const [taskResponse, memberResponse] = await Promise.all([
      fetch("/api/v1/task-occurrences", { cache: "no-store" }),
      fetch("/api/v1/family-members", { cache: "no-store" }),
    ]);
    if (!taskResponse.ok || !memberResponse.ok) return;
    const taskResult = await taskResponse.json() as { tasks: TaskItem[] };
    const memberResult = await memberResponse.json() as { members: FamilyMember[] };
    setTasks(taskResult.tasks);
    setMembers(memberResult.members);
  }, [auth]);

  const refreshTaskTrash = useCallback(async () => {
    if (!auth?.permissions.admin) { setTrash([]); return; }
    const response = await fetch("/api/admin/task-trash", { cache: "no-store" });
    if (response.ok) setTrash((await response.json() as { tasks: TaskTrashEntry[] }).tasks);
  }, [auth]);

  const refreshTaskHistory = useCallback(async () => {
    if (!auth?.permissions.admin) { setHistory([]); return; }
    const response = await fetch("/api/admin/task-history", { cache: "no-store" });
    if (response.ok) setHistory((await response.json() as { tasks: TaskItem[] }).tasks);
  }, [auth]);

  const refreshSkippedTasks = useCallback(async () => {
    if (!auth?.permissions.admin) { setSkipped([]); return; }
    const response = await fetch("/api/admin/task-skips", { cache: "no-store" });
    if (response.ok) setSkipped((await response.json() as { tasks: TaskItem[] }).tasks);
  }, [auth]);

  useEffect(() => { void refreshTasks(); const timer=window.setInterval(()=>void refreshTasks(),60_000);return()=>window.clearInterval(timer); }, [refreshTasks]);
  useEffect(() => { void refreshTaskTrash(); }, [refreshTaskTrash]);
  useEffect(() => { void refreshTaskHistory(); }, [refreshTaskHistory]);
  useEffect(() => { void refreshSkippedTasks(); }, [refreshSkippedTasks]);

  if (!auth) return null;

  const visibleTasks=focusMemberId?tasks.filter(item=>item.assignees.includes(focusMemberId)):tasks;
  const visibleHistory=focusMemberId?history.filter(item=>item.assignees.includes(focusMemberId)):history;
  const visibleSkipped=focusMemberId?skipped.filter(item=>item.assignees.includes(focusMemberId)):skipped;
  const visibleTrash=focusMemberId?trash.filter(entry=>entry.task.assignees.includes(focusMemberId)):trash;

  function toggleAssignee(id: string) {
    setForm((current) => {
      const assignees = current.assignees.includes(id)
        ? current.assignees.filter((value) => value !== id)
        : [...current.assignees, id];
      const notificationRecipients = current.id
        ? current.notificationRecipients
        : assignees.length === 1 && assignees[0] === "dad" ? ["dad"]
          : assignees.length === 1 && assignees[0] === "mom" ? ["mom"] : ["dad", "mom"];
      return { ...current, assignees, notificationRecipients };
    });
  }

  function toggleTaskNotificationRecipient(owner: string) {
    setForm((current) => ({
      ...current,
      notificationRecipients: current.notificationRecipients.includes(owner)
        ? current.notificationRecipients.filter((value) => value !== owner)
        : [...current.notificationRecipients, owner],
    }));
  }

  function editTask(item: TaskItem) {
    const hour = Math.floor((item.dueMinute ?? 540) / 60);
    const minute = (item.dueMinute ?? 540) % 60;
    setForm({
      id: item.id, title: item.title, notes: item.notes ?? "", priority: item.priority,
      dueKind: item.dueKind, dueDate: item.dueDate ?? emptyTaskForm().dueDate,
      dueTime: `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`,
      assignees: item.assignees, repeatKind: item.repeatKind,
      startsOn: item.startsOn ?? emptyTaskForm().startsOn, endsOn: item.endsOn ?? "",
      weekdays: item.weekdays ?? [], version: item.version,
      notificationEnabled: item.dueKind !== "none", notificationLeadMinutes: 60,
      notificationDateHour: 9, notificationRecipients: ["dad", "mom"],
    });
    setShowForm(true);
    void fetch(`/api/v1/tasks/${encodeURIComponent(item.id)}/notification`, { cache: "no-store" })
      .then(async (response) => response.ok ? await response.json() as {
        enabled: boolean; timedLeadMinutes: number; dateHour: number; recipients: string[];
      } : null)
      .then((setting) => {
        if (!setting) return;
        setForm((current) => current.id === item.id ? {
          ...current, notificationEnabled: setting.enabled,
          notificationLeadMinutes: setting.timedLeadMinutes,
          notificationDateHour: setting.dateHour,
          notificationRecipients: setting.recipients,
        } : current);
      });
  }

  async function saveTask() {
    setBusy(true);
    setError("");
    try {
      const [hour, minute] = form.dueTime.split(":").map(Number);
      const response = await fetch(form.id ? `/api/v1/tasks/${encodeURIComponent(form.id)}` : "/api/v1/tasks", {
        method: form.id ? "PUT" : "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
        body: JSON.stringify({
          title: form.title, notes: form.notes, priority: form.priority,
          dueKind: form.dueKind, dueDate: form.dueKind === "none" ? "" : form.dueDate,
          dueMinute: hour * 60 + minute, assignees: form.assignees,
          repeatKind: form.repeatKind, startsOn: form.repeatKind === "none" ? "" : form.startsOn,
          endsOn: form.repeatKind === "none" ? "" : form.endsOn,
          weekdays: form.repeatKind === "weekly" ? form.weekdays : [], version: form.version,
        }),
      });
      if (!response.ok) {
        const result = await response.json() as { message?: string };
        throw new Error(result.message ?? "할 일을 저장하지 못했습니다.");
      }
      const saved = await response.json() as TaskItem;
      const notificationResponse = await fetch(`/api/v1/tasks/${encodeURIComponent(saved.id)}/notification`, {
        method: "PUT",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
        body: JSON.stringify({
          enabled: form.dueKind !== "none" && form.notificationEnabled,
          timedLeadMinutes: form.notificationLeadMinutes,
          dateHour: form.notificationDateHour,
          recipients: form.notificationRecipients,
        }),
      });
      if (!notificationResponse.ok) {
        throw new Error("할 일은 저장됐지만 알림 설정을 저장하지 못했습니다.");
      }
      setForm(emptyTaskForm());
      setShowForm(false);
      await refreshTasks();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "할 일을 저장하지 못했습니다.");
    } finally {
      setBusy(false);
    }
  }

  async function setCompleted(item: TaskItem, completed: boolean) {
    setBusy(true);
    const action = completed ? "complete" : "reopen";
    const response = await fetch(`/api/v1/tasks/${encodeURIComponent(item.id)}/occurrences/${encodeURIComponent(item.occurrenceKey)}/${action}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
      body: "{}",
    });
    if (!response.ok) setError("완료 상태를 변경하지 못했습니다.");
    await Promise.all([refreshTasks(), refreshTaskHistory()]);
    setBusy(false);
  }

  async function setTaskOccurrenceSkipped(item: TaskItem, shouldSkip: boolean) {
    if (shouldSkip && !window.confirm("이 반복 할 일의 이번 회차를 건너뛸까요?")) return;
    setBusy(true);
    const response = await fetch(`/api/v1/tasks/${encodeURIComponent(item.id)}/occurrences/${encodeURIComponent(item.occurrenceKey)}/${shouldSkip ? "skip" : "unskip"}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
      body: "{}",
    });
    if (!response.ok) setError(shouldSkip ? "이번 회차를 건너뛰지 못했습니다." : "건너뛴 회차를 복원하지 못했습니다.");
    await Promise.all([refreshTasks(), refreshSkippedTasks()]);
    setBusy(false);
  }

  async function deleteTask() {
    if (!form.id || !window.confirm("이 할 일을 휴지통으로 이동할까요?")) return;
    setBusy(true);
    const response = await fetch(`/api/v1/tasks/${encodeURIComponent(form.id)}`, {
      method: "DELETE", headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
      body: JSON.stringify({ version: form.version }),
    });
    if (!response.ok) setError("할 일을 삭제하지 못했습니다.");
    setForm(emptyTaskForm()); setShowForm(false);
    await Promise.all([refreshTasks(), refreshTaskTrash()]); setBusy(false);
  }

  async function changeTaskTrash(entry: TaskTrashEntry, action: "restore" | "delete") {
    if (action === "delete" && !window.confirm(`‘${entry.task.title}’ 할 일을 영구 삭제할까요?`)) return;
    setBusy(true);
    const response = await fetch(action === "restore" ? `/api/admin/task-trash/${encodeURIComponent(entry.task.id)}/restore` : `/api/admin/task-trash/${encodeURIComponent(entry.task.id)}`, {
      method: action === "restore" ? "POST" : "DELETE",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": readCookie("family_dashboard_csrf") },
      body: JSON.stringify({ version: entry.task.version }),
    });
    if (!response.ok) setError(action === "restore" ? "할 일을 복원하지 못했습니다." : "할 일을 영구 삭제하지 못했습니다.");
    await Promise.all([refreshTasks(), refreshTaskTrash()]); setBusy(false);
  }

  return (
    <section className="task-card">
      <div className="schedule-heading">
        <div><p className="label">오늘의 할 일</p><strong>{visibleTasks.length === 0 ? "등록된 할 일 없음" : `${visibleTasks.length}개 할 일`}</strong></div>
        {auth.permissions.admin && <button type="button" onClick={() => { const next=emptyTaskForm();setForm({...next,assignees:focusMemberId?[focusMemberId]:[],notificationRecipients:focusMemberId==="dad"||focusMemberId==="mom"?[focusMemberId]:["dad","mom"]}); setShowForm((value) => !value); }}>{showForm ? "입력 닫기" : "할 일 추가"}</button>}
      </div>
      {showForm && auth.permissions.admin && (
        <form className="task-form" onSubmit={(event) => { event.preventDefault(); void saveTask(); }}>
          <label><span>제목</span><input value={form.title} maxLength={200} onChange={(event) => setForm({ ...form, title: event.target.value })} /></label>
          <label><span>중요도</span><select value={form.priority} onChange={(event) => setForm({ ...form, priority: event.target.value as TaskForm["priority"] })}><option value="normal">보통</option><option value="important">중요</option></select></label>
          <label className="full-width"><span>메모</span><textarea value={form.notes} maxLength={2000} onChange={(event) => setForm({ ...form, notes: event.target.value })} /></label>
          <label><span>마감</span><select value={form.dueKind} onChange={(event) => { const dueKind = event.target.value as TaskForm["dueKind"]; setForm({ ...form, dueKind, notificationEnabled: dueKind !== "none" }); }}><option value="none">마감 없음</option><option value="date">날짜까지</option><option value="datetime">날짜와 시각</option></select></label>
          {form.dueKind !== "none" && <label><span>마감일</span><input type="date" value={form.dueDate} onChange={(event) => setForm({ ...form, dueDate: event.target.value })} /></label>}
          {form.dueKind === "datetime" && <label><span>마감 시각</span><input type="time" value={form.dueTime} onChange={(event) => setForm({ ...form, dueTime: event.target.value })} /></label>}
          <label><span>반복</span><select value={form.repeatKind} onChange={(event) => setForm({ ...form, repeatKind: event.target.value as TaskForm["repeatKind"] })}><option value="none">반복 없음</option><option value="daily">매일</option><option value="weekly">매주</option></select></label>
          {form.repeatKind !== "none" && <label><span>반복 시작일</span><input type="date" value={form.startsOn} onChange={(event) => setForm({ ...form, startsOn: event.target.value })} /></label>}
          {form.repeatKind !== "none" && <label><span>반복 종료일 (선택)</span><input type="date" min={form.startsOn} value={form.endsOn} onChange={(event) => setForm({ ...form, endsOn: event.target.value })} /></label>}
          {form.repeatKind === "weekly" && <fieldset><legend>반복 요일</legend><div className="participant-options">{[[0,"일"],[1,"월"],[2,"화"],[3,"수"],[4,"목"],[5,"금"],[6,"토"]].map(([day,label]) => <label key={day}><input type="checkbox" checked={form.weekdays.includes(day as number)} onChange={() => setForm({ ...form, weekdays: form.weekdays.includes(day as number) ? form.weekdays.filter((value) => value !== day) : [...form.weekdays, day as number] })} /><span>{label}</span></label>)}</div></fieldset>}
          <fieldset><legend>담당 가족</legend><div className="participant-options">{members.map((member) => <label key={member.id}><input type="checkbox" checked={form.assignees.includes(member.id)} onChange={() => toggleAssignee(member.id)} /><span>{member.displayName}</span></label>)}</div></fieldset>
          {form.dueKind !== "none" && <fieldset><legend>할 일 알림</legend><label className="recurrence-toggle"><input type="checkbox" checked={form.notificationEnabled} onChange={(event) => setForm({ ...form, notificationEnabled: event.target.checked })} /><span>부모 모바일 알림 사용</span></label>{form.notificationEnabled && <><label><span>{form.dueKind === "datetime" ? "마감 전 알림" : "알림 시각"}</span>{form.dueKind === "datetime" ? <select value={form.notificationLeadMinutes} onChange={(event) => setForm({ ...form, notificationLeadMinutes: Number(event.target.value) })}><option value={0}>마감 시각</option><option value={10}>10분 전</option><option value={30}>30분 전</option><option value={60}>1시간 전</option><option value={1440}>하루 전</option></select> : <select value={form.notificationDateHour} onChange={(event) => setForm({ ...form, notificationDateHour: Number(event.target.value) })}>{Array.from({ length: 24 }, (_, hour) => <option key={hour} value={hour}>{String(hour).padStart(2, "0")}:00</option>)}</select>}</label><div className="participant-options" aria-label="알림 수신자"><label><input type="checkbox" checked={form.notificationRecipients.includes("dad")} onChange={() => toggleTaskNotificationRecipient("dad")} /><span>아빠 모바일</span></label><label><input type="checkbox" checked={form.notificationRecipients.includes("mom")} onChange={() => toggleTaskNotificationRecipient("mom")} /><span>엄마 모바일</span></label></div></>}</fieldset>}
          <div className="button-row"><button type="submit" disabled={busy}>{form.id ? "할 일 수정" : "할 일 저장"}</button>{form.id && <button type="button" className="danger" disabled={busy} onClick={() => void deleteTask()}>할 일 삭제</button>}<button type="button" className="secondary" onClick={() => { setShowForm(false); setForm(emptyTaskForm()); }}>취소</button></div>
        </form>
      )}
      {error && <p className="form-error">{error}</p>}
      <div className="task-list">{visibleTasks.map((item) => <article key={`${item.id}-${item.occurrenceKey}`} className={`${item.completedAt ? "task-completed" : ""} ${item.overdue ? "task-overdue" : ""}`}><div><strong>{item.title}</strong><span>{item.priority === "important" ? "중요" : "보통"}{item.overdue ? " · 기한 지남" : ""}{item.repeatKind !== "none" ? ` · ${item.repeatKind === "daily" ? "매일" : "매주"}` : ""}</span>{item.notes && <span>{item.notes}</span>}</div>{auth.permissions.admin && <div className="button-row"><button type="button" className={item.completedAt ? "secondary" : ""} disabled={busy} onClick={() => void setCompleted(item, !item.completedAt)}>{item.completedAt ? "완료 취소" : "완료"}</button>{item.repeatKind !== "none" && !item.completedAt && <button type="button" className="secondary" disabled={busy} onClick={() => void setTaskOccurrenceSkipped(item, true)}>이번 회차 건너뛰기</button>}<button type="button" className="secondary" onClick={() => editTask(item)}>수정</button></div>}</article>)}</div>
      {auth.permissions.admin && <div className="schedule-trash"><div className="management-heading"><div><p className="label">완료 기록</p><strong>{visibleHistory.length === 0 ? "비어 있음" : `${visibleHistory.length}개 기록`}</strong></div><button type="button" className="secondary" onClick={() => setShowHistory((value) => !value)}>{showHistory ? "기록 닫기" : "기록 보기"}</button></div>{showHistory && <div className="trash-list">{visibleHistory.map((item) => <article key={`${item.id}-${item.occurrenceKey}`}><div><strong>{item.title}</strong><span>{item.completedAt ? new Date(item.completedAt).toLocaleString("ko-KR") : "완료"}</span></div><button type="button" className="secondary" disabled={busy} onClick={() => void setCompleted(item, false)}>미완료로 복원</button></article>)}</div>}</div>}
      {auth.permissions.admin && <div className="schedule-trash"><div className="management-heading"><div><p className="label">건너뛴 회차</p><strong>{visibleSkipped.length === 0 ? "비어 있음" : `${visibleSkipped.length}개 회차`}</strong></div><button type="button" className="secondary" onClick={() => setShowSkipped((value) => !value)}>{showSkipped ? "목록 닫기" : "목록 보기"}</button></div>{showSkipped && <div className="trash-list">{visibleSkipped.map((item) => <article key={`${item.id}-${item.occurrenceKey}`}><div><strong>{item.title}</strong><span>{item.occurrenceKey}</span></div><button type="button" className="secondary" disabled={busy} onClick={() => void setTaskOccurrenceSkipped(item, false)}>회차 복원</button></article>)}</div>}</div>}
      {auth.permissions.admin && <div className="schedule-trash"><div className="management-heading"><div><p className="label">할 일 휴지통</p><strong>{visibleTrash.length === 0 ? "비어 있음" : `${visibleTrash.length}개 할 일`}</strong></div><button type="button" className="secondary" onClick={() => setShowTrash((value) => !value)}>{showTrash ? "휴지통 닫기" : "휴지통 보기"}</button></div>{showTrash && <div className="trash-list">{visibleTrash.map((entry) => <article key={entry.task.id}><div><strong>{entry.task.title}</strong><span>복원 가능 {new Date(entry.restoreUntil).toLocaleDateString("ko-KR")}까지</span></div><div className="button-row"><button type="button" className="secondary" onClick={() => void changeTaskTrash(entry, "restore")}>복원</button><button type="button" className="danger" onClick={() => void changeTaskTrash(entry, "delete")}>영구 삭제</button></div></article>)}</div>}</div>}
    </section>
  );
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
      {auth.permissions.admin && auth.permissions.canManageDevices && (
        <>
          <ParentAccountManagement />
          <DeviceManagement currentDeviceId={auth.device.id} />
        </>
      )}
      <button type="button" className="secondary" disabled={submitting} onClick={async()=>{if(!window.confirm("이 브라우저에서 로그아웃할까요?"))return;setSubmitting(true);try{const response=await fetch("/api/auth/logout",{method:"POST",headers:{"X-CSRF-Token":readCookie("family_dashboard_csrf")}});if(!response.ok)throw new Error("로그아웃하지 못했습니다.");window.location.assign("/")}catch(caught){setError(caught instanceof Error?caught.message:"로그아웃하지 못했습니다.");setSubmitting(false)}}}>이 브라우저 로그아웃</button>
      {error && <p className="form-error" role="alert">{error}</p>}
    </section>
  );
}

type ParentAccountStatus={owner:"dad"|"mom";configured:boolean;enabled:boolean};
function ParentAccountManagement(){
  const [accounts,setAccounts]=useState<ParentAccountStatus[]>([]);const [owner,setOwner]=useState<"dad"|"mom">("mom");const [password,setPassword]=useState("");const [confirmation,setConfirmation]=useState("");const [message,setMessage]=useState("");const [error,setError]=useState("");const [busy,setBusy]=useState(false);
  const load=useCallback(async()=>{const response=await fetch("/api/admin/parent-accounts",{cache:"no-store"});if(response.ok)setAccounts((await response.json()).accounts||[])},[]);useEffect(()=>{void load()},[load]);
  async function save(event:FormEvent){event.preventDefault();setError("");setMessage("");if(password.length<12){setError("비밀번호는 12자 이상 입력하세요.");return}if(password!==confirmation){setError("비밀번호 확인이 일치하지 않습니다.");return}setBusy(true);const response=await fetch(`/api/admin/parent-accounts/${owner}`,{method:"PUT",headers:{"Content-Type":"application/json","X-CSRF-Token":readCookie("family_dashboard_csrf")},body:JSON.stringify({password})});setBusy(false);if(!response.ok){setError("로그인 비밀번호를 저장하지 못했습니다.");return}setPassword("");setConfirmation("");setMessage(`${owner==="dad"?"아빠":"엄마"} 로그인 비밀번호를 저장했습니다.`);void load()}
  async function disable(target:"dad"|"mom"){if(!window.confirm(`${target==="dad"?"아빠":"엄마"} 로그인을 끌까요? 비밀번호로 로그인한 기존 기기도 함께 로그아웃됩니다.`))return;const response=await fetch(`/api/admin/parent-accounts/${target}`,{method:"DELETE",headers:{"X-CSRF-Token":readCookie("family_dashboard_csrf")}});if(response.ok){setMessage("로그인을 끄고 기존 로그인 기기도 로그아웃했습니다.");void load()}else setError("로그인을 끄지 못했습니다.")}
  return <section className="parent-account-card"><div className="management-heading"><div><p className="label">부모 로그인 계정</p><strong>외부 브라우저용 비밀번호</strong></div></div><div className="account-status-list">{accounts.map(account=><div key={account.owner}><span>{account.owner==="dad"?"아빠":"엄마"}</span><strong>{account.configured&&account.enabled?"사용 중":account.configured?"꺼짐":"설정 전"}</strong>{account.configured&&account.enabled&&<button type="button" className="danger" onClick={()=>void disable(account.owner)}>끄기</button>}</div>)}</div><form className="domain-form" onSubmit={save}><label><span>설정할 계정</span><select value={owner} onChange={event=>setOwner(event.target.value as "dad"|"mom")}><option value="dad">아빠</option><option value="mom">엄마</option></select></label><label><span>새 비밀번호</span><input type="password" autoComplete="new-password" minLength={12} maxLength={128} value={password} onChange={event=>setPassword(event.target.value)} required/></label><label><span>비밀번호 확인</span><input type="password" autoComplete="new-password" minLength={12} maxLength={128} value={confirmation} onChange={event=>setConfirmation(event.target.value)} required/></label><p className="security-note">각자 다른 12자 이상의 비밀번호를 사용하세요. 이 값은 서버에 Argon2id 해시로만 저장됩니다.</p>{message&&<p className="form-success" role="status">{message}</p>}{error&&<p className="form-error" role="alert">{error}</p>}<button type="submit" disabled={busy}>{busy?"안전하게 저장 중…":`${owner==="dad"?"아빠":"엄마"} 비밀번호 저장`}</button></form></section>
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

function ParentLogin({onComplete}:{onComplete:()=>Promise<void>}){
  const [owner,setOwner]=useState<"dad"|"mom">("dad");const [password,setPassword]=useState("");const [deviceName,setDeviceName]=useState(()=>`${navigator.platform||"휴대폰"} 브라우저`);const [busy,setBusy]=useState(false);const [error,setError]=useState("");
  async function submit(event:FormEvent){event.preventDefault();setError("");setBusy(true);try{const response=await fetch("/api/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({owner,password,deviceName})});if(response.status===429)throw new Error("로그인 시도가 여러 번 실패해 15분 동안 잠겼습니다.");if(response.status===400)throw new Error("비밀번호는 12자 이상이며 기기 이름도 입력해야 합니다.");if(!response.ok)throw new Error("계정이 설정되지 않았거나 비밀번호가 올바르지 않습니다.");setPassword("");await onComplete()}catch(caught){setError(caught instanceof Error?caught.message:"로그인하지 못했습니다.")}finally{setBusy(false)}}
  return <main className="setup-layout parent-login"><header className="setup-intro"><p className="eyebrow">FAMILY DASHBOARD</p><h1>가족 대시보드 로그인</h1><p className="subtitle">아빠 또는 엄마 계정으로 로그인하세요. Tailscale 앱은 필요하지 않습니다.</p></header><form className="setup-card" onSubmit={submit}><label><span>사용자</span><select value={owner} onChange={event=>setOwner(event.target.value as "dad"|"mom")}><option value="dad">아빠</option><option value="mom">엄마</option></select></label><label><span>비밀번호</span><input type="password" autoComplete="current-password" minLength={12} maxLength={128} value={password} onChange={event=>setPassword(event.target.value)} autoFocus required/></label><label><span>이 기기 이름</span><input autoComplete="off" maxLength={80} value={deviceName} onChange={event=>setDeviceName(event.target.value)} required/></label>{error&&<p className="form-error" role="alert">{error}</p>}<button type="submit" disabled={busy}>{busy?"로그인 중…":"로그인"}</button><div className="enrollment-links"><a className="enrollment-link secondary" href="/enroll">기존 등록 코드 방식 사용</a></div><p className="security-note">공용 기기에서는 로그인하지 마세요. 일정 수정 등 관리 작업에는 기존 관리자 PIN이 추가로 필요합니다.</p></form></main>
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
