import { useState } from 'react'
import { Alert, App as AntApp, Button, Flex, Input, Modal, Typography } from 'antd'
import { useTranslation } from 'react-i18next'
import type { CreateTokenResponse } from '../api/tokens'

export interface TokenSecretModalProps {
  /** The just-created token (with its one-time secret), or null when closed. */
  token: CreateTokenResponse | null
  onClose: () => void
}

/**
 * Shows a freshly minted token's bearer secret exactly once (F-020 API
 * contract: "the SECRET is shown once"). The backend never returns the
 * secret again — only its SHA-256 hash is stored — so this is the only
 * chance to copy it. `destroyOnHidden` drops the secret from the DOM the
 * instant the modal closes, and the parent clears `token` in `onClose` so
 * reopening never resurrects the old value.
 */
export function TokenSecretModal({ token, onClose }: TokenSecretModalProps) {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    if (token === null) {
      return
    }
    navigator.clipboard
      .writeText(token.token)
      .then(() => {
        setCopied(true)
        void message.success(t('tokens.secretModal.copied'))
      })
      .catch(() => {
        void message.error(t('tokens.secretModal.copyFailed'))
      })
  }

  return (
    <Modal
      open={token !== null}
      title={t('tokens.secretModal.title')}
      closable={false}
      keyboard={false}
      mask={{ closable: false }}
      destroyOnHidden
      footer={
        <Button type="primary" onClick={onClose}>
          {t('tokens.secretModal.close')}
        </Button>
      }
    >
      <Flex vertical gap="middle">
        <Alert
          type="warning"
          showIcon
          title={t('tokens.secretModal.warningTitle')}
          description={t('tokens.secretModal.warning')}
        />
        {token !== null && (
          <>
            <Typography.Text>
              {t('tokens.secretModal.name', { name: token.name })}
            </Typography.Text>
            <Flex gap="small">
              <Input readOnly value={token.token} className="tabular" aria-label={t('tokens.secretModal.title')} />
              <Button onClick={handleCopy}>
                {copied ? t('tokens.secretModal.copiedButton') : t('tokens.actions.copy')}
              </Button>
            </Flex>
          </>
        )}
      </Flex>
    </Modal>
  )
}
