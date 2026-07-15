import { useState } from 'react'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Empty,
  Flex,
  Popconfirm,
  Space,
  Table,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlayCircleOutlined, StopOutlined } from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/client'
import type { Sequence, SequenceInput } from '../api/sequences'
import { countNodes } from '../components/sequences/stepTree'
import { SequenceFormModal } from '../components/sequences/SequenceFormModal'
import { RunPanel } from '../components/sequences/RunPanel'
import {
  useCreateSequence,
  useDeleteSequence,
  useLiveRun,
  useRunSequence,
  useSequenceLiveInvalidation,
  useSequencesQuery,
  useStopSequence,
  useUpdateSequence,
} from '../hooks/useSequences'

/**
 * Programmable sequences (F-022): a table of saved sequences with per-row
 * Run/Stop, a create/edit modal built on the StepTreeEditor outline, and a
 * live Run panel while a run is active (driven by GET /sequences/active plus
 * the `sequenceProgress` WS stream). Only one run at a time — Run buttons on
 * other sequences are disabled while a run is active.
 */
export function SequencesPage() {
  const { t } = useTranslation()
  const { message } = AntApp.useApp()
  useSequenceLiveInvalidation()

  const sequencesQuery = useSequencesQuery()
  const liveRun = useLiveRun()
  const createMutation = useCreateSequence()
  const updateMutation = useUpdateSequence()
  const deleteMutation = useDeleteSequence()
  const runMutation = useRunSequence()
  const stopMutation = useStopSequence()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Sequence | null>(null)

  const runningId = liveRun.run.active ? liveRun.run.sequenceId : null
  const anyRunActive = liveRun.run.active

  const storageUnavailable =
    (sequencesQuery.error instanceof ApiError &&
      sequencesQuery.error.code === 'storage_unavailable') ||
    (liveRun.error instanceof ApiError && liveRun.error.code === 'storage_unavailable')

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (seq: Sequence) => {
    setEditing(seq)
    setModalOpen(true)
  }

  const handleSubmit = (input: SequenceInput) => {
    if (editing !== null) {
      updateMutation.mutate(
        { id: editing.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            void message.success(t('sequences.saved'))
          },
        },
      )
    } else {
      createMutation.mutate(input, {
        onSuccess: () => {
          setModalOpen(false)
          void message.success(t('sequences.created'))
        },
      })
    }
  }

  const handleDelete = (seq: Sequence) => {
    deleteMutation.mutate(seq.id, {
      onSuccess: () => {
        void message.success(t('sequences.deleted'))
      },
    })
  }

  const handleRun = (seq: Sequence) => {
    runMutation.mutate(seq.id, {
      onSuccess: () => {
        void message.success(t('sequences.runStarted', { name: seq.name }))
      },
    })
  }

  const handleStop = () => {
    stopMutation.mutate(undefined, {
      onSuccess: () => {
        void message.success(t('sequences.stopped'))
      },
    })
  }

  const columns: ColumnsType<Sequence> = [
    {
      title: t('sequences.table.name'),
      dataIndex: 'name',
      key: 'name',
      sorter: (a, b) => a.name.localeCompare(b.name),
    },
    {
      title: t('sequences.table.steps'),
      key: 'steps',
      width: 100,
      render: (_, seq) => <span className="tabular">{countNodes(seq.steps)}</span>,
    },
    {
      title: t('sequences.table.run'),
      key: 'run',
      width: 130,
      render: (_, seq) =>
        runningId === seq.id ? (
          <Button
            danger
            size="small"
            icon={<StopOutlined />}
            loading={stopMutation.isPending}
            onClick={handleStop}
          >
            {t('sequences.actions.stop')}
          </Button>
        ) : (
          <Button
            type="primary"
            size="small"
            icon={<PlayCircleOutlined />}
            disabled={anyRunActive}
            loading={runMutation.isPending && runMutation.variables === seq.id}
            onClick={() => handleRun(seq)}
          >
            {t('sequences.actions.run')}
          </Button>
        ),
    },
    {
      title: t('sequences.table.actions'),
      key: 'actions',
      render: (_, seq) => (
        <Space size="small" wrap>
          <Button size="small" onClick={() => openEdit(seq)}>
            {t('sequences.actions.edit')}
          </Button>
          <Popconfirm
            title={t('sequences.deleteConfirm.title', { name: seq.name })}
            description={t('sequences.deleteConfirm.content')}
            okText={t('sequences.deleteConfirm.ok')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => handleDelete(seq)}
          >
            <Button
              size="small"
              danger
              disabled={runningId === seq.id}
              loading={deleteMutation.isPending && deleteMutation.variables === seq.id}
            >
              {t('sequences.actions.delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Flex vertical gap="middle">
      <Flex align="center" justify="space-between" wrap gap="small">
        <Typography.Title level={4} style={{ margin: 0 }}>
          {t('sequences.title')}
        </Typography.Title>
        <Button type="primary" onClick={openCreate}>
          {t('sequences.addButton')}
        </Button>
      </Flex>

      {storageUnavailable && (
        <Alert
          type="error"
          showIcon
          message={t('sequences.errors.storageUnavailableTitle')}
          description={t('sequences.errors.storageUnavailable')}
          action={
            <Button
              size="small"
              onClick={() => {
                void sequencesQuery.refetch()
              }}
            >
              {t('common.retry')}
            </Button>
          }
        />
      )}

      {liveRun.run.active && (
        <RunPanel run={liveRun.run} stopping={stopMutation.isPending} onStop={handleStop} />
      )}

      <Card>
        <Table<Sequence>
          rowKey="id"
          columns={columns}
          dataSource={sequencesQuery.data?.items ?? []}
          loading={sequencesQuery.isLoading}
          pagination={false}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: <Empty description={t('sequences.empty')} /> }}
        />
      </Card>

      <SequenceFormModal
        open={modalOpen}
        editing={editing}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        onCancel={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />
    </Flex>
  )
}
