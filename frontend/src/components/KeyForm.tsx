import {FolderOpen} from 'lucide-react'
import {api} from '../api'
import type {SftpKeyRequest} from '../api/v2'
import {useToast} from './Toast'
import {Button, Checkbox, Field, Input, PasswordInput, Select} from './ui'

export interface KeyFormState {
  mode: 'none' | 'generate' | 'paste'
  keyType: 'ed25519' | 'rsa4096'
  passphrase: string
  comment: string
  outputDir: string
  exportPpk: boolean
  publicKey: string
}
export const emptyKeyForm = (allowNone = true): KeyFormState => ({
  mode: allowNone ? 'none' : 'generate', keyType: 'ed25519', passphrase: '', comment: '', outputDir: '', exportPpk: true, publicKey: '',
})
export const keyFormValid = (k: KeyFormState) =>
  k.mode === 'none' || (k.mode === 'generate' ? !!k.outputDir : /^(ssh-|ecdsa-)/.test(k.publicKey.trim()))
export const toKeyRequest = (k: KeyFormState): SftpKeyRequest | null =>
  k.mode === 'none' ? null : {mode: k.mode, keyType: k.keyType, passphrase: k.passphrase, comment: k.comment, outputDir: k.outputDir, exportPpk: k.exportPpk, publicKey: k.publicKey.trim()}

export default function KeyForm({value, onChange, allowNone = true}: {value: KeyFormState; onChange: (v: KeyFormState) => void; allowNone?: boolean}) {
  const toast = useToast()
  const set = (p: Partial<KeyFormState>) => onChange({...value, ...p})
  const pick = async () => {
    try { const d = await api.pickDirectory('Carpeta donde guardar la llave privada'); if (d) set({outputDir: d}) } catch (e) { toast.error(e) }
  }
  return (
    <fieldset className="space-y-3 rounded-md border border-line p-3">
      <legend className="px-1 text-xs font-medium text-muted">Llave de acceso</legend>
      <Field label="Tipo de acceso con llave">
        {id => <Select id={id} value={value.mode} onChange={e => set({mode: e.target.value as KeyFormState['mode']})}>
          {allowNone && <option value="none">Sin llave</option>}
          <option value="generate">Generar una llave nueva</option>
          <option value="paste">Pegar una llave pública existente</option>
        </Select>}
      </Field>
      {value.mode === 'generate' && <>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Algoritmo">{id => <Select id={id} value={value.keyType} onChange={e => set({keyType: e.target.value as KeyFormState['keyType']})}>
            <option value="ed25519">ed25519 (recomendado)</option><option value="rsa4096">RSA 4096</option></Select>}</Field>
          <Field label="Comentario (opcional)">{id => <Input id={id} value={value.comment} onChange={e => set({comment: e.target.value})} placeholder="usuario@equipo"/>}</Field>
        </div>
        <Field label="Frase de paso (opcional)" hint="Protege la llave privada. Si la olvidas no se puede recuperar.">
          {id => <PasswordInput id={id} value={value.passphrase} onChange={v => set({passphrase: v})}/>}
        </Field>
        <Field label="Carpeta donde guardar la llave privada">
          {id => <div className="flex gap-2"><Input id={id} readOnly value={value.outputDir} placeholder="Ninguna carpeta seleccionada"/>
            <Button icon={<FolderOpen size={15}/>} onClick={pick}>Elegir</Button></div>}
        </Field>
        <Checkbox label="Exportar también .ppk para WinSCP/PuTTY" checked={value.exportPpk} onChange={e => set({exportPpk: e.target.checked})}/>
      </>}
      {value.mode === 'paste' && (
        <Field label="Llave pública" hint="Una línea que empiece por ssh-ed25519, ssh-rsa o ecdsa-...">
          {id => <textarea id={id} rows={3} value={value.publicKey} onChange={e => set({publicKey: e.target.value})}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 font-mono text-xs" placeholder="ssh-ed25519 AAAA... comentario"/>}
        </Field>
      )}
    </fieldset>
  )
}
