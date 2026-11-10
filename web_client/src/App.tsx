import { useEffect, useState } from 'react';
import ricovrIcon from './imports/Copy_of_RICOVR_Icon.png';
import ricovrLogo from './imports/Copy_of_RICOVR_Logo.png';
import xalivaLogo from './imports/Xaliva_Logo.png';
import ricovrVectorLogo from './imports/LOGO.svg';

type ConnectionType = 'usb' | 'cloud' | 'local';
type ConnectionStatus = 'connected' | 'disconnected' | 'connecting';

interface LogFile {
  id: string;
  name: string;
  size: string;
  modified: Date;
  deviceId: string;
  synced: boolean;
}

interface LogEntry {
  time: string;
  channel: number;
  raw: number;
  absorbance: number;
  temperature: number;
  flag: 'OK' | 'WARN' | 'ERR';
}

interface DeviceInfo {
  serialNumber: string;
  model: string;
  firmwareVersion: string;
  hardwareRevision: string;
  lastCalibration: string;
  operatorId: string;
}

// ── Sample data ──────────────────────────────────────────────
const LOG_FILES: LogFile[] = [
  { id: '1', name: '10244280_1.txt',          size: '14 KB', modified: new Date(Date.now() - 2*3600000),  deviceId: '10244280', synced: true },
  { id: '2', name: '10244280_2.txt',          size: '12 KB', modified: new Date(Date.now() - 4*3600000),  deviceId: '10244280', synced: true },
  { id: '3', name: '10244280_3.txt',          size: '18 KB', modified: new Date(Date.now() - 6*3600000),  deviceId: '10244280', synced: false },
  { id: '4', name: '10244280_4.txt',          size: '9 KB',  modified: new Date(Date.now() - 9*3600000),  deviceId: '10244280', synced: false },
  { id: '5', name: '10244280_devicelog.txt',  size: '4 KB',  modified: new Date(Date.now() - 12*3600000), deviceId: '10244280', synced: true },
  { id: '6', name: 'logtrack.txt',            size: '2 KB',  modified: new Date(Date.now() - 24*3600000), deviceId: '10244280', synced: true },
  { id: '7', name: 'temp_devicelog.txt',      size: '1 KB',  modified: new Date(Date.now() - 48*3600000), deviceId: '10244280', synced: false },
];

const DEVICE_INFO: DeviceInfo = {
  serialNumber: '10244280',
  model: 'Ricovr P-FAB RX-400',
  firmwareVersion: '2.4.1',
  hardwareRevision: 'Rev C',
  lastCalibration: 'Sep 26, 2026 — 09:14 AM',
  operatorId: 'T-04412',
};

const LOG_ENTRIES: LogEntry[] = [
  { time: '09:14:02', channel: 1, raw: 31820, absorbance: 0.214, temperature: 36.9, flag: 'OK' },
  { time: '09:14:08', channel: 2, raw: 29441, absorbance: 0.301, temperature: 37.0, flag: 'OK' },
  { time: '09:14:14', channel: 3, raw: 18320, absorbance: 0.748, temperature: 37.1, flag: 'OK' },
  { time: '09:14:20', channel: 4, raw: 12804, absorbance: 1.042, temperature: 37.2, flag: 'WARN' },
  { time: '09:14:26', channel: 5, raw: 31100, absorbance: 0.238, temperature: 37.0, flag: 'OK' },
  { time: '09:14:32', channel: 6, raw: 30980, absorbance: 0.244, temperature: 36.8, flag: 'OK' },
  { time: '09:14:38', channel: 7, raw: 9440,  absorbance: 1.204, temperature: 37.3, flag: 'ERR' },
  { time: '09:14:44', channel: 8, raw: 29810, absorbance: 0.278, temperature: 37.0, flag: 'OK' },
];

const CHART_BARS = [0.214, 0.301, 0.748, 1.042, 0.238, 0.244, 1.204, 0.278];
const MAX_ABS = 1.5;

function formatRelative(date: Date): string {
  const diff = Math.floor((Date.now() - date.getTime()) / 60000);
  if (diff < 60) return `${diff}m ago`;
  if (diff < 1440) return `${Math.floor(diff / 60)}h ago`;
  return `${Math.floor(diff / 1440)}d ago`;
}

// ── Styles ───────────────────────────────────────────────────
const navy = '#162d55';
const teal = '#2ec4b6';
const tealDark = '#1a9e97';
const bg = '#f0f4f8';
const white = '#ffffff';
const border = '#dde3ec';
const muted = '#6b7a99';
const label = '#94a3b8';

function LoadingScreen({ exiting }: { exiting: boolean }) {
  return (
    <div
      className={`loading-screen${exiting ? ' loading-screen--exiting' : ''}`}
      role="status"
      aria-live="polite"
      aria-label="Loading Ricovr Log Analyzer"
    >
      <div className="loading-screen__glow loading-screen__glow--left" />
      <div className="loading-screen__glow loading-screen__glow--right" />

      <div className="loading-screen__content">
        <div className="loading-screen__mark" aria-hidden="true">
          <img src={ricovrIcon} alt="" />
          <span className="loading-screen__orbit" />
        </div>

        <div className="loading-screen__logo-plate">
          <img src={ricovrVectorLogo} alt="Ricovr" />
        </div>
        <div className="loading-screen__product">LOG ANALYZER</div>

        <div className="loading-screen__progress" aria-hidden="true">
          <span />
        </div>

        <div className="loading-screen__message">
          <span className="loading-screen__pulse" aria-hidden="true" />
          Preparing your device workspace
        </div>
      </div>

      <div className="loading-screen__footer">Secure device monitoring</div>
    </div>
  );
}

interface LoginScreenProps {
  onLogin: () => void;
}

function LoginScreen({ onLogin }: LoginScreenProps) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [isSigningIn, setIsSigningIn] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!email.trim() || !password.trim()) {
      setError('Enter your Ricovr email and password to continue.');
      return;
    }

    setError('');
    setIsSigningIn(true);
    window.setTimeout(onLogin, 900);
  };

  return (
    <main className="login">
      <div className="login__grid" aria-hidden="true" />
      <div className="login__orb login__orb--top" aria-hidden="true" />
      <div className="login__orb login__orb--bottom" aria-hidden="true" />
      <img className="login__backdrop-mark" src={ricovrIcon} alt="" aria-hidden="true" />

      <section className="login__card" aria-labelledby="login-title">
        <div className="login__accent" aria-hidden="true" />
        <div className="login__brand">
          <img className="login__logo" src={ricovrLogo} alt="Ricovr" />
          <div className="login__product-label">LOG ANALYZER</div>
        </div>

        <div className="login__heading">
          <div className="login__access-label"><span /> AUTHORIZED ACCESS</div>
          <h1 className="login__title" id="login-title">Lab technician sign in</h1>
          <p className="login__subtitle">Enter your credentials to access the device workspace.</p>
        </div>

        <form className="login__form" onSubmit={handleSubmit}>
          <label htmlFor="email">Work email</label>
          <div className="login__input-wrap">
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M4 6h16v12H4V6Z M4 7l8 6 8-6" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
            </svg>
            <input
              id="email"
              type="email"
              autoComplete="email"
              placeholder="name@ricovr.com"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </div>

          <div className="login__label-row">
            <label htmlFor="password">Password</label>
            <button type="button">Forgot password?</button>
          </div>
          <div className="login__input-wrap">
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <rect x="5" y="10" width="14" height="10" rx="2" stroke="currentColor" strokeWidth="1.6" />
              <path d="M8 10V7a4 4 0 0 1 8 0v3" stroke="currentColor" strokeWidth="1.6" />
            </svg>
            <input
              id="password"
              type={showPassword ? 'text' : 'password'}
              autoComplete="current-password"
              placeholder="Enter your password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
            <button
              className="login__show-password"
              type="button"
              onClick={() => setShowPassword((current) => !current)}
              aria-label={showPassword ? 'Hide password' : 'Show password'}
            >
              {showPassword ? 'Hide' : 'Show'}
            </button>
          </div>

          {error && <div className="login__error" role="alert">{error}</div>}

          <button className="login__submit" type="submit" disabled={isSigningIn}>
            {isSigningIn ? (
              <><span className="login__spinner" />Authenticating</>
            ) : (
              <>Sign in <span aria-hidden="true">→</span></>
            )}
          </button>
        </form>

        <div className="login__card-footer">
          <div className="login__security">
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M12 3 5.5 5.6v5.2c0 4.2 2.7 8 6.5 10.2 3.8-2.2 6.5-6 6.5-10.2V5.6L12 3Z" stroke="currentColor" strokeWidth="1.5" />
              <path d="m9.2 12 1.8 1.8 3.8-4" stroke="currentColor" strokeWidth="1.5" />
            </svg>
            Encrypted session
          </div>
          <img className="login__xaliva" src={xalivaLogo} alt="Xaliva" />
        </div>
      </section>

      <div className="login__legal">RICOVR INC. · AUTHORIZED PERSONNEL ONLY</div>
    </main>
  );
}

export default function App() {
  const [isLoading, setIsLoading] = useState(true);
  const [isLoaderExiting, setIsLoaderExiting] = useState(false);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [connectionType, setConnectionType] = useState<ConnectionType>('usb');
  const [status, setStatus] = useState<ConnectionStatus>('connected');
  const [port, setPort] = useState('COM9');
  const [selectedFile, setSelectedFile] = useState<LogFile | null>(LOG_FILES[0]);
  const [activePanel, setActivePanel] = useState<'info' | 'log' | 'viz'>('log');

  const isConnected = status === 'connected';

  useEffect(() => {
    const exitTimer = window.setTimeout(() => setIsLoaderExiting(true), 1600);
    const loadingTimer = window.setTimeout(() => setIsLoading(false), 1950);

    return () => {
      window.clearTimeout(exitTimer);
      window.clearTimeout(loadingTimer);
    };
  }, []);

  const handleConnect = () => {
    if (status === 'disconnected') {
      setStatus('connecting');
      setTimeout(() => setStatus('connected'), 1200);
    } else {
      setStatus('disconnected');
    }
  };

  if (isLoading) {
    return <LoadingScreen exiting={isLoaderExiting} />;
  }

  if (!isAuthenticated) {
    return <LoginScreen onLogin={() => setIsAuthenticated(true)} />;
  }

  return (
    <div style={{ minHeight: '100vh', background: bg, fontFamily: 'var(--font-sans)', color: navy }}>

      {/* ── Top Nav ── */}
      <header style={{ background: white, height: '64px', display: 'flex', alignItems: 'center', padding: '0 28px', justifyContent: 'space-between', borderBottom: `1px solid ${border}` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <img src={ricovrLogo} alt="Ricovr" style={{ width: '126px', height: '28px', objectFit: 'contain', objectPosition: 'left center' }} />
          <span style={{ color: teal, fontSize: '10px', fontWeight: 700, letterSpacing: '0.1em', border: `1px solid rgba(46,196,182,0.4)`, borderRadius: '4px', padding: '2px 7px', marginLeft: '2px' }}>
            LOG ANALYZER
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '20px' }}>
          <div style={{ textAlign: 'right' }}>
            <div style={{ color: navy, fontSize: '12.5px', fontFamily: 'var(--font-mono)', fontWeight: 600 }}>
              {new Date().toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}
            </div>
            <div style={{ color: label, fontSize: '10.5px' }}>
              {new Date().toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })}
            </div>
          </div>
          <button
            onClick={() => setIsAuthenticated(false)}
            title="Sign out"
            style={{ width: '34px', height: '34px', borderRadius: '50%', border: 'none', background: 'linear-gradient(135deg, #1a9e97, #162d55)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: white, fontSize: '11px', fontWeight: 700, cursor: 'pointer' }}
          >
            HT
          </button>
        </div>
      </header>

      {/* ── Connection Bar ── */}
      <div style={{ background: white, borderBottom: `1px solid ${border}`, padding: '0 28px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0', height: '48px' }}>

          {/* Source tabs */}
          {(['usb', 'cloud', 'local'] as ConnectionType[]).map(type => (
            <button
              key={type}
              onClick={() => setConnectionType(type)}
              style={{
                height: '100%', padding: '0 18px', border: 'none', background: 'none',
                fontSize: '13px', fontWeight: connectionType === type ? 600 : 400,
                color: connectionType === type ? navy : muted,
                borderBottom: connectionType === type ? `2px solid ${teal}` : '2px solid transparent',
                cursor: 'pointer', transition: 'all 0.15s', marginBottom: '-1px',
              }}
            >
              {type === 'usb' ? 'USB Connection' : type === 'cloud' ? 'From Cloud' : 'This PC'}
            </button>
          ))}

          {/* Separator */}
          <div style={{ flex: 1 }} />

          {/* Connection controls */}
          {connectionType === 'usb' && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <span style={{ fontSize: '12px', color: label, fontWeight: 500 }}>Serial Port</span>
              <select
                value={port}
                onChange={e => setPort(e.target.value)}
                style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', border: `1px solid ${border}`, borderRadius: '6px', padding: '4px 8px', color: navy, background: white, outline: 'none' }}
              >
                <option>COM9</option><option>COM3</option><option>COM7</option>
              </select>

              {/* Status pill */}
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: '5px', fontSize: '11.5px', fontWeight: 600, padding: '3px 10px', borderRadius: '20px',
                background: isConnected ? '#ecfdf5' : '#f1f5f9',
                color: isConnected ? '#059669' : '#94a3b8',
                border: `1px solid ${isConnected ? '#a7f3d0' : '#e2e8f0'}`,
              }}>
                <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: isConnected ? '#10b981' : '#94a3b8', display: 'inline-block' }} />
                {status === 'connecting' ? 'Connecting…' : isConnected ? 'Connected' : 'Disconnected'}
              </span>

              <button
                onClick={handleConnect}
                style={{ fontSize: '12.5px', fontWeight: 600, padding: '5px 14px', borderRadius: '6px', border: `1px solid ${border}`, background: white, color: navy, cursor: 'pointer', transition: 'all 0.15s' }}
              >
                {isConnected ? 'Disconnect' : 'Connect'}
              </button>
              <button
                disabled={!isConnected}
                style={{ fontSize: '12.5px', fontWeight: 600, padding: '5px 14px', borderRadius: '6px', border: 'none', background: isConnected ? navy : '#e2e8f0', color: isConnected ? white : '#94a3b8', cursor: isConnected ? 'pointer' : 'not-allowed', transition: 'all 0.15s' }}
              >
                Fetch Logs
              </button>
            </div>
          )}
        </div>
      </div>

      {/* ── Main Layout ── */}
      <div style={{ display: 'grid', gridTemplateColumns: '300px 1fr', gap: '0', minHeight: 'calc(100vh - 113px)' }}>

        {/* ── Left: Log File List ── */}
        <div style={{ background: white, borderRight: `1px solid ${border}`, display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '16px 20px 10px', borderBottom: `1px solid ${border}` }}>
            <div style={{ fontSize: '10.5px', color: label, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.09em', marginBottom: '10px' }}>
              Recently Viewed
            </div>
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '8px 0' }}>
            {LOG_FILES.map(file => {
              const isSelected = selectedFile?.id === file.id;
              return (
                <button
                  key={file.id}
                  onClick={() => { setSelectedFile(file); setActivePanel('log'); }}
                  style={{
                    width: '100%', display: 'flex', alignItems: 'center', gap: '10px',
                    padding: '10px 20px', border: 'none', background: isSelected ? '#eef3fb' : 'transparent',
                    textAlign: 'left', cursor: 'pointer', transition: 'background 0.1s',
                    borderLeft: isSelected ? `3px solid ${teal}` : '3px solid transparent',
                  }}
                  onMouseEnter={e => { if (!isSelected) (e.currentTarget as HTMLElement).style.background = '#f8fafc'; }}
                  onMouseLeave={e => { if (!isSelected) (e.currentTarget as HTMLElement).style.background = 'transparent'; }}
                >
                  {/* File icon */}
                  <svg width="16" height="18" viewBox="0 0 16 18" fill="none" style={{ flexShrink: 0 }}>
                    <path d="M2 0h8l6 6v12H2V0z" fill={isSelected ? teal : '#e2e8f0'} />
                    <path d="M10 0l6 6h-6V0z" fill={isSelected ? tealDark : '#cbd5e1'} />
                    <rect x="4" y="9" width="8" height="1.2" rx="0.6" fill={isSelected ? white : '#94a3b8'} />
                    <rect x="4" y="12" width="6" height="1.2" rx="0.6" fill={isSelected ? white : '#94a3b8'} />
                  </svg>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: '13px', fontWeight: isSelected ? 600 : 400, color: isSelected ? navy : '#334155', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {file.name}
                    </div>
                    <div style={{ fontSize: '11px', color: label, marginTop: '1px', display: 'flex', gap: '6px' }}>
                      <span>{file.size}</span>
                      <span>·</span>
                      <span>{formatRelative(file.modified)}</span>
                    </div>
                  </div>
                  {!file.synced && (
                    <span style={{ width: '6px', height: '6px', borderRadius: '50%', background: '#f59e0b', flexShrink: 0 }} title="Not synced" />
                  )}
                </button>
              );
            })}
          </div>

          {/* Bottom actions */}
          <div style={{ padding: '14px 20px', borderTop: `1px solid ${border}`, display: 'flex', gap: '8px' }}>
            <button style={{ flex: 1, fontSize: '12px', fontWeight: 600, padding: '7px 0', borderRadius: '7px', border: `1px solid ${border}`, background: white, color: navy, cursor: 'pointer' }}>
              Save All to Disk
            </button>
            <button style={{ flex: 1, fontSize: '12px', fontWeight: 600, padding: '7px 0', borderRadius: '7px', border: 'none', background: navy, color: white, cursor: 'pointer' }}>
              Sync All to Cloud
            </button>
          </div>
        </div>

        {/* ── Right: Detail Panels ── */}
        <div style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>

          {selectedFile ? (
            <>
              {/* Panel tabs */}
              <div style={{ background: white, borderBottom: `1px solid ${border}`, padding: '0 24px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', height: '48px' }}>
                <div style={{ display: 'flex', gap: '0', height: '100%' }}>
                  {(['info', 'log', 'viz'] as const).map(tab => (
                    <button
                      key={tab}
                      onClick={() => setActivePanel(tab)}
                      style={{
                        height: '100%', padding: '0 18px', border: 'none', background: 'none',
                        fontSize: '13px', fontWeight: activePanel === tab ? 600 : 400,
                        color: activePanel === tab ? navy : muted,
                        borderBottom: activePanel === tab ? `2px solid ${teal}` : '2px solid transparent',
                        cursor: 'pointer', transition: 'all 0.12s', marginBottom: '-1px',
                      }}
                    >
                      {tab === 'info' ? 'Device Information' : tab === 'log' ? 'Log Information' : 'Data Visualization'}
                    </button>
                  ))}
                </div>
                <div style={{ fontSize: '12px', fontFamily: 'var(--font-mono)', color: label }}>
                  {selectedFile.name}
                </div>
              </div>

              {/* Panel content */}
              <div style={{ flex: 1, overflowY: 'auto', padding: '24px' }}>

                {/* ── Device Information ── */}
                {activePanel === 'info' && (
                  <div>
                    <h2 style={{ fontSize: '16px', fontWeight: 700, color: navy, margin: '0 0 20px', letterSpacing: '-0.01em' }}>Device Information</h2>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', maxWidth: '640px' }}>
                      {[
                        ['Serial Number', DEVICE_INFO.serialNumber],
                        ['Model', DEVICE_INFO.model],
                        ['Firmware Version', DEVICE_INFO.firmwareVersion],
                        ['Hardware Revision', DEVICE_INFO.hardwareRevision],
                        ['Last Calibration', DEVICE_INFO.lastCalibration],
                        ['Operator ID', DEVICE_INFO.operatorId],
                      ].map(([k, v]) => (
                        <div key={k} style={{ background: white, border: `1px solid ${border}`, borderRadius: '10px', padding: '14px 16px' }}>
                          <div style={{ fontSize: '10.5px', color: label, textTransform: 'uppercase', letterSpacing: '0.08em', fontWeight: 600, marginBottom: '5px' }}>{k}</div>
                          <div style={{ fontSize: '14px', fontWeight: 600, color: navy, fontFamily: k === 'Serial Number' || k === 'Firmware Version' || k === 'Operator ID' ? 'var(--font-mono)' : 'inherit' }}>{v}</div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* ── Log Information ── */}
                {activePanel === 'log' && (
                  <div>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
                      <h2 style={{ fontSize: '16px', fontWeight: 700, color: navy, margin: 0, letterSpacing: '-0.01em' }}>Log Information</h2>
                      <div style={{ display: 'flex', gap: '6px' }}>
                        {(['OK', 'WARN', 'ERR'] as const).map(flag => {
                          const count = LOG_ENTRIES.filter(e => e.flag === flag).length;
                          const styles: Record<string, { bg: string; color: string; border: string }> = {
                            OK:   { bg: '#ecfdf5', color: '#059669', border: '#a7f3d0' },
                            WARN: { bg: '#fffbeb', color: '#b45309', border: '#fde68a' },
                            ERR:  { bg: '#fef2f2', color: '#b91c1c', border: '#fecaca' },
                          };
                          const s = styles[flag];
                          return (
                            <span key={flag} style={{ fontSize: '11.5px', fontWeight: 700, padding: '3px 9px', borderRadius: '20px', background: s.bg, color: s.color, border: `1px solid ${s.border}` }}>
                              {count} {flag}
                            </span>
                          );
                        })}
                      </div>
                    </div>

                    {/* Log table */}
                    <div style={{ background: white, border: `1px solid ${border}`, borderRadius: '10px', overflow: 'hidden' }}>
                      <div style={{ display: 'grid', gridTemplateColumns: '90px 70px 100px 120px 110px 70px', padding: '10px 16px', background: '#f8fafc', borderBottom: `1px solid ${border}` }}>
                        {['Time', 'Channel', 'Raw ADC', 'Absorbance', 'Temp (°C)', 'Flag'].map(h => (
                          <div key={h} style={{ fontSize: '10.5px', color: label, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.07em' }}>{h}</div>
                        ))}
                      </div>
                      {LOG_ENTRIES.map((entry, i) => {
                        const isLast = i === LOG_ENTRIES.length - 1;
                        const flagStyles: Record<string, React.CSSProperties> = {
                          OK:   { color: '#059669', background: '#ecfdf5', padding: '1px 7px', borderRadius: '4px', fontWeight: 700, fontSize: '10.5px' },
                          WARN: { color: '#b45309', background: '#fffbeb', padding: '1px 7px', borderRadius: '4px', fontWeight: 700, fontSize: '10.5px' },
                          ERR:  { color: '#b91c1c', background: '#fef2f2', padding: '1px 7px', borderRadius: '4px', fontWeight: 700, fontSize: '10.5px' },
                        };
                        return (
                          <div
                            key={entry.time + entry.channel}
                            style={{
                              display: 'grid', gridTemplateColumns: '90px 70px 100px 120px 110px 70px',
                              padding: '10px 16px', borderBottom: isLast ? 'none' : `1px solid #f1f5f9`,
                              background: entry.flag === 'ERR' ? '#fff8f8' : entry.flag === 'WARN' ? '#fffdf5' : white,
                              alignItems: 'center',
                            }}
                          >
                            <div style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', color: '#334155', fontWeight: 500 }}>{entry.time}</div>
                            <div style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', color: muted }}>CH{entry.channel}</div>
                            <div style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', color: navy }}>{entry.raw.toLocaleString()}</div>
                            <div style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', color: navy, fontWeight: 600 }}>{entry.absorbance.toFixed(3)} AU</div>
                            <div style={{ fontSize: '12.5px', fontFamily: 'var(--font-mono)', color: navy }}>{entry.temperature.toFixed(1)}</div>
                            <div><span style={flagStyles[entry.flag]}>{entry.flag}</span></div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}

                {/* ── Data Visualization ── */}
                {activePanel === 'viz' && (
                  <div>
                    <h2 style={{ fontSize: '16px', fontWeight: 700, color: navy, margin: '0 0 6px', letterSpacing: '-0.01em' }}>Data Visualization</h2>
                    <p style={{ fontSize: '13px', color: muted, margin: '0 0 24px' }}>Absorbance by channel — {selectedFile.name}</p>

                    {/* Bar chart */}
                    <div style={{ background: white, border: `1px solid ${border}`, borderRadius: '12px', padding: '24px', maxWidth: '640px' }}>
                      <div style={{ display: 'flex', alignItems: 'flex-end', gap: '12px', height: '180px', paddingBottom: '8px', borderBottom: `1px solid ${border}` }}>
                        {CHART_BARS.map((val, i) => {
                          const entry = LOG_ENTRIES[i];
                          const pct = (val / MAX_ABS) * 100;
                          const barColor = entry.flag === 'ERR' ? '#ef4444' : entry.flag === 'WARN' ? '#f59e0b' : teal;
                          return (
                            <div key={i} style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '4px', height: '100%', justifyContent: 'flex-end' }}>
                              <div style={{ fontSize: '10px', fontFamily: 'var(--font-mono)', color: muted, fontWeight: 600 }}>{val.toFixed(3)}</div>
                              <div style={{ width: '100%', height: `${pct}%`, background: barColor, borderRadius: '4px 4px 0 0', transition: 'height 0.3s', minHeight: '4px', opacity: 0.85 }} />
                            </div>
                          );
                        })}
                      </div>
                      {/* X axis labels */}
                      <div style={{ display: 'flex', gap: '12px', marginTop: '8px' }}>
                        {CHART_BARS.map((_, i) => (
                          <div key={i} style={{ flex: 1, textAlign: 'center', fontSize: '10.5px', fontFamily: 'var(--font-mono)', color: label }}>CH{i + 1}</div>
                        ))}
                      </div>

                      {/* Legend */}
                      <div style={{ marginTop: '20px', paddingTop: '16px', borderTop: `1px solid ${border}`, display: 'flex', gap: '16px', flexWrap: 'wrap' }}>
                        <div style={{ fontSize: '12px', color: muted }}>
                          <span style={{ fontWeight: 600, color: navy }}>Peak:</span> CH7 — 1.204 AU
                        </div>
                        <div style={{ fontSize: '12px', color: muted }}>
                          <span style={{ fontWeight: 600, color: navy }}>Mean:</span> 0.534 AU
                        </div>
                        <div style={{ fontSize: '12px', color: muted }}>
                          <span style={{ fontWeight: 600, color: navy }}>Threshold:</span> ≥ 1.0 AU flagged
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </>
          ) : (
            <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', gap: '12px', color: label }}>
              <svg width="40" height="40" viewBox="0 0 40 40" fill="none">
                <rect x="6" y="4" width="22" height="32" rx="3" fill="#e2e8f0"/>
                <rect x="10" y="14" width="14" height="2" rx="1" fill="#94a3b8"/>
                <rect x="10" y="19" width="10" height="2" rx="1" fill="#94a3b8"/>
                <rect x="10" y="24" width="12" height="2" rx="1" fill="#94a3b8"/>
              </svg>
              <p style={{ fontSize: '13.5px', margin: 0 }}>Select a log file to inspect</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
