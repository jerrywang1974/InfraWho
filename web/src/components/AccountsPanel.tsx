import { useEffect, useRef, useState, type FormEvent } from 'react'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { AccountSummary } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { clearClipboard, writeClipboard } from '../lib/clipboard'
import { formatAuthType } from '../lib/format'
import { StepUpModal } from './StepUpModal'

const AUTH_TYPES = [
  { value: 'password', label: '密碼' },
  { value: 'ssh_private_key', label: 'SSH 私鑰' },
  { value: 'api_token', label: 'API 權杖' },
  { value: 'other', label: '其他' },
] as const

const DEFAULT_CLIPBOARD_CLEAR_SEC = 30

type Props = {
  assetId: string
  accounts: AccountSummary[]
  canWrite: boolean
  canReveal: boolean
  onChanged: () => void
}

type Revealed = {
  accountId: string
  secret: string
  visible: boolean
}

export function AccountsPanel({ assetId, accounts, canWrite, canReveal, onChanged }: Props) {
  const { user, refreshUserQuiet } = useAuth()
  const [showCreate, setShowCreate] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [revealed, setRevealed] = useState<Revealed | null>(null)
  const [clipboardWarn, setClipboardWarn] = useState<string | null>(null)
  const [clearAfterSec, setClearAfterSec] = useState(DEFAULT_CLIPBOARD_CLEAR_SEC)
  const [autoClearClipboard, setAutoClearClipboard] = useState(true)
  const [stepUpOpen, setStepUpOpen] = useState(false)
  const pendingRevealId = useRef<string | null>(null)
  const clipboardTimer = useRef<number | null>(null)
  const clipboardPendingClear = useRef(false)

  // Drop React plaintext on unmount; best-effort clipboard clear if a clear was scheduled.
  useEffect(() => {
    return () => {
      if (clipboardTimer.current != null) {
        window.clearTimeout(clipboardTimer.current)
        clipboardTimer.current = null
      }
      if (clipboardPendingClear.current) {
        clipboardPendingClear.current = false
        void clearClipboard()
      }
    }
  }, [])

  function clearReveal() {
    setRevealed(null)
  }

  function scheduleClipboardClear(seconds: number) {
    if (clipboardTimer.current != null) {
      window.clearTimeout(clipboardTimer.current)
      clipboardTimer.current = null
    }
    if (seconds <= 0) {
      clipboardPendingClear.current = false
      return
    }
    clipboardPendingClear.current = true
    clipboardTimer.current = window.setTimeout(() => {
      clipboardPendingClear.current = false
      void clearClipboard().then((ok) => {
        setClipboardWarn(
          ok
            ? `已嘗試在 ${seconds} 秒後清除剪貼簿（若瀏覽器允許）。`
            : `已過 ${seconds} 秒；無法自動清除剪貼簿，請手動覆蓋。`,
        )
      })
      clipboardTimer.current = null
    }, seconds * 1000)
  }

  async function doReveal(accountId: string) {
    setBusyId(accountId)
    setError(null)
    setClipboardWarn(null)
    try {
      const res = await api.revealAccount(accountId)
      setRevealed({ accountId, secret: res.secret, visible: false })
      // Quiet me() refresh for step_up_active hint — must not flip Auth loading.
      void refreshUserQuiet()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'step_up_required') {
        pendingRevealId.current = accountId
        setStepUpOpen(true)
        return
      }
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('無法揭示密文')
      }
    } finally {
      setBusyId(null)
    }
  }

  async function onCopy(accountId: string) {
    const secret = revealed?.accountId === accountId ? revealed.secret : null
    if (!secret) {
      setError('請先揭示後再複製')
      return
    }
    const ok = await writeClipboard(secret)
    if (!ok) {
      setClipboardWarn('無法寫入剪貼簿（權限或非安全內容環境）。請改用顯示後手動選取。')
      return
    }
    setClipboardWarn(
      '已複製到剪貼簿。注意：剪貼簿可能殘留明文，請勿貼到聊天／郵件；關閉分頁不會自動清除系統剪貼簿。',
    )
    if (autoClearClipboard) {
      scheduleClipboardClear(clearAfterSec)
    }
  }

  async function onDelete(id: string) {
    if (!window.confirm('確定刪除此帳號與其密文？此操作無法復原。')) return
    setBusyId(id)
    setError(null)
    try {
      await api.deleteAccount(id)
      if (revealed?.accountId === id) clearReveal()
      onChanged()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '刪除失敗')
    } finally {
      setBusyId(null)
    }
  }

  function onRotateDone(accountId: string) {
    if (revealed?.accountId === accountId) clearReveal()
    onChanged()
  }

  return (
    <section className="panel panel-wide">
      <div className="panel-header">
        <h2>帳號／憑證</h2>
        {canWrite ? (
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => setShowCreate((v) => !v)}
          >
            {showCreate ? '關閉表單' : '新增帳號'}
          </button>
        ) : null}
      </div>

      <p className="muted hint">
        密文預設遮罩；不會寫入網址或 localStorage。揭示需操作者以上角色與二次確認。
        {user?.step_up_active ? '（目前二次確認仍有效）' : ''}
      </p>

      {showCreate && canWrite ? (
        <CreateAccountForm
          assetId={assetId}
          onCreated={() => {
            setShowCreate(false)
            onChanged()
          }}
          onCancel={() => setShowCreate(false)}
        />
      ) : null}

      {error ? <p className="error">{error}</p> : null}
      {clipboardWarn ? <p className="warn-text">{clipboardWarn}</p> : null}

      {canReveal ? (
        <div className="reveal-options">
          <label className="check">
            <input
              type="checkbox"
              checked={autoClearClipboard}
              onChange={(e) => setAutoClearClipboard(e.target.checked)}
            />
            <span>複製後嘗試清除剪貼簿</span>
          </label>
          <label className="inline-num">
            秒數
            <input
              type="number"
              min={5}
              max={300}
              value={clearAfterSec}
              disabled={!autoClearClipboard}
              onChange={(e) =>
                setClearAfterSec(Math.min(300, Math.max(5, Number(e.target.value) || 30)))
              }
            />
          </label>
        </div>
      ) : null}

      {accounts.length === 0 ? (
        <p className="muted">尚無帳號。</p>
      ) : (
        <ul className="item-list">
          {accounts.map((acc) => {
            const isRevealed = revealed?.accountId === acc.id
            return (
              <li key={acc.id} className="item-card">
                <div className="item-main">
                  <div className="item-title">
                    <code>{acc.username}</code>
                    <span className="tag">{formatAuthType(acc.auth_type)}</span>
                    {!acc.has_secret ? (
                      <span className="status-chip">密碼未知</span>
                    ) : (
                      <span className="status-chip ok">已保存密文</span>
                    )}
                  </div>
                  {acc.description ? <p className="muted clamp-2">{acc.description}</p> : null}
                  {acc.last_rotated_at ? (
                    <p className="muted hint">最近輪替：{acc.last_rotated_at}</p>
                  ) : null}

                  {isRevealed ? (
                    <div className="secret-box">
                      <div className="row-actions">
                        <button
                          type="button"
                          className="btn"
                          onClick={() =>
                            setRevealed((r) => (r ? { ...r, visible: !r.visible } : r))
                          }
                        >
                          {revealed.visible ? '隱藏' : '顯示'}
                        </button>
                        <button type="button" className="btn" onClick={() => void onCopy(acc.id)}>
                          複製
                        </button>
                        <button type="button" className="btn btn-ghost" onClick={clearReveal}>
                          清除畫面
                        </button>
                      </div>
                      <pre className="secret-value" aria-label="已揭示的密文">
                        {revealed.visible ? revealed.secret : '••••••••••••'}
                      </pre>
                    </div>
                  ) : null}
                </div>

                <div className="item-actions">
                  {canReveal && acc.has_secret ? (
                    <button
                      type="button"
                      className="btn"
                      disabled={busyId === acc.id}
                      onClick={() => void doReveal(acc.id)}
                    >
                      {busyId === acc.id ? '揭示中…' : isRevealed ? '重新揭示' : '揭示'}
                    </button>
                  ) : null}
                  {canWrite ? <EditAccountForm account={acc} onDone={onChanged} /> : null}
                  {canWrite && acc.has_secret ? (
                    <RotateSecretForm
                      accountId={acc.id}
                      authType={acc.auth_type}
                      onDone={() => onRotateDone(acc.id)}
                    />
                  ) : null}
                  {canWrite && !acc.has_secret ? (
                    <RotateSecretForm
                      accountId={acc.id}
                      authType={acc.auth_type}
                      onDone={() => onRotateDone(acc.id)}
                      label="設定密文"
                    />
                  ) : null}
                  {canWrite ? (
                    <button
                      type="button"
                      className="btn btn-danger"
                      disabled={busyId === acc.id}
                      onClick={() => void onDelete(acc.id)}
                    >
                      刪除
                    </button>
                  ) : null}
                </div>
              </li>
            )
          })}
        </ul>
      )}

      <StepUpModal
        open={stepUpOpen}
        title="揭示前二次確認"
        hint="揭示憑證需重新輸入登入密碼。成功後約 5 分鐘內可繼續揭示。"
        onCancel={() => {
          setStepUpOpen(false)
          pendingRevealId.current = null
        }}
        onSuccess={() => {
          setStepUpOpen(false)
          void refreshUserQuiet()
          const id = pendingRevealId.current
          pendingRevealId.current = null
          if (id) void doReveal(id)
        }}
      />
    </section>
  )
}

function secretInputIsMasked(authType: string): boolean {
  return authType === 'password' || authType === 'api_token'
}

function CreateAccountForm({
  assetId,
  onCreated,
  onCancel,
}: {
  assetId: string
  onCreated: () => void
  onCancel: () => void
}) {
  const [username, setUsername] = useState('')
  const [authType, setAuthType] = useState('password')
  const [description, setDescription] = useState('')
  const [secret, setSecret] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.createAccount(assetId, {
        username: username.trim(),
        auth_type: authType,
        description: description.trim(),
        secret: secret ? secret : undefined,
      })
      setSecret('')
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '建立失敗')
    } finally {
      setBusy(false)
    }
  }

  const masked = secretInputIsMasked(authType)

  return (
    <form className="form-card nested-form" onSubmit={onSubmit}>
      <h3>新增帳號</h3>
      <div className="form-grid">
        <label>
          使用者名稱
          <input value={username} onChange={(e) => setUsername(e.target.value)} required />
        </label>
        <label>
          驗證類型
          <select value={authType} onChange={(e) => setAuthType(e.target.value)}>
            {AUTH_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        <label className="span-2">
          說明
          <input value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <label className="span-2">
          密文（可留空＝密碼未知）
          {masked ? (
            <div className="secret-input-row">
              <input
                type={showSecret ? 'text' : 'password'}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                autoComplete="off"
              />
              <button
                type="button"
                className="btn"
                onClick={() => setShowSecret((v) => !v)}
              >
                {showSecret ? '隱藏' : '顯示'}
              </button>
            </div>
          ) : (
            <textarea
              rows={3}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              autoComplete="off"
            />
          )}
        </label>
      </div>
      {error ? <p className="error">{error}</p> : null}
      <div className="row-actions">
        <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={busy}>
          取消
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? '建立中…' : '建立'}
        </button>
      </div>
    </form>
  )
}

function EditAccountForm({
  account,
  onDone,
}: {
  account: AccountSummary
  onDone: () => void
}) {
  const [open, setOpen] = useState(false)
  const [username, setUsername] = useState(account.username)
  const [description, setDescription] = useState(account.description)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.patchAccount(account.id, {
        username: username.trim(),
        description,
      })
      setOpen(false)
      onDone()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '更新失敗')
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        className="btn"
        onClick={() => {
          setUsername(account.username)
          setDescription(account.description)
          setOpen(true)
        }}
      >
        編輯
      </button>
    )
  }

  return (
    <form className="nested-form rotate-form" onSubmit={onSubmit}>
      <label>
        使用者名稱
        <input value={username} onChange={(e) => setUsername(e.target.value)} required />
      </label>
      <label>
        說明
        <input value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>
      {account.has_secret ? (
        <p className="muted hint">已有密文時不可只改驗證類型；請用輪替密文。</p>
      ) : null}
      {error ? <p className="error">{error}</p> : null}
      <div className="row-actions">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => setOpen(false)}
          disabled={busy}
        >
          取消
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? '儲存中…' : '儲存'}
        </button>
      </div>
    </form>
  )
}

function RotateSecretForm({
  accountId,
  authType,
  onDone,
  label = '輪替密文',
}: {
  accountId: string
  authType: string
  onDone: () => void
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const [secret, setSecret] = useState('')
  const [nextAuthType, setNextAuthType] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const effectiveType = nextAuthType || authType
  const masked = secretInputIsMasked(effectiveType)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.rotateSecret(accountId, {
        secret,
        auth_type: nextAuthType || undefined,
      })
      setSecret('')
      setOpen(false)
      onDone()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '輪替失敗')
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <button type="button" className="btn" onClick={() => setOpen(true)}>
        {label}
      </button>
    )
  }

  return (
    <form className="nested-form rotate-form" onSubmit={onSubmit}>
      <label>
        新密文
        {masked ? (
          <div className="secret-input-row">
            <input
              type={showSecret ? 'text' : 'password'}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              required
              autoComplete="off"
            />
            <button type="button" className="btn" onClick={() => setShowSecret((v) => !v)}>
              {showSecret ? '隱藏' : '顯示'}
            </button>
          </div>
        ) : (
          <textarea
            rows={2}
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            required
            autoComplete="off"
          />
        )}
      </label>
      <label>
        可選：同時變更類型
        <select value={nextAuthType} onChange={(e) => setNextAuthType(e.target.value)}>
          <option value="">（維持原類型）</option>
          {AUTH_TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </label>
      {error ? <p className="error">{error}</p> : null}
      <div className="row-actions">
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => {
            setOpen(false)
            setSecret('')
          }}
          disabled={busy}
        >
          取消
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy || !secret}>
          {busy ? '儲存中…' : '儲存'}
        </button>
      </div>
    </form>
  )
}
