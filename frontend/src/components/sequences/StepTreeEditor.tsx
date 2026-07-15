import { Alert, Button, Card, Flex, InputNumber, Select, Tag, Typography } from 'antd'
import {
  AimOutlined,
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  GroupOutlined,
  PlusOutlined,
  RetweetOutlined,
  RiseOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import type {
  AutomationCondition,
  AutomationConditionType,
} from '../../api/automation'
import type {
  LoopNode,
  RampNode,
  RampTarget,
  SequenceNode,
  SequenceNodeType,
  SetHoldNode,
} from '../../api/sequences'
import {
  MAX_AMPS,
  MAX_NESTING_DEPTH,
  MAX_NODE_COUNT,
  MAX_VOLTS,
  appendChildAt,
  appendTopLevel,
  conditionOfType,
  countNodes,
  type FieldError,
  moveNodeAt,
  newNode,
  nodeFieldErrors,
  programIssues,
  removeNodeAt,
  updateNodeAt,
  wrapNodeInLoop,
} from './stepTree'
import './StepTreeEditor.css'

/** Immutable tree mutations wired to the editor's `onChange`, shared by rows. */
interface Ops {
  update: (path: number[], updater: (node: SequenceNode) => SequenceNode) => void
  remove: (path: number[]) => void
  move: (path: number[], dir: -1 | 1) => void
  wrap: (path: number[]) => void
  /** Append a new node of `type` to `parentPath` ([] = top level). */
  add: (parentPath: number[], type: SequenceNodeType) => void
  /** True once the program is at MAX_NODE_COUNT — all "add" actions disabled. */
  atMax: boolean
}

function errorMessage(t: TFunction, error: FieldError): string {
  switch (error.code) {
    case 'required':
      return t('sequences.form.required')
    case 'positive':
      return t('sequences.form.positive')
    case 'min':
      return t('sequences.form.min', { min: error.min })
    case 'range':
      return t('sequences.form.range', { min: error.min, max: error.max })
  }
}

function fieldError(errors: FieldError[], field: string): FieldError | undefined {
  return errors.find((e) => e.field === field)
}

interface NumFieldProps {
  label: string
  value: number
  min?: number
  max?: number
  step?: number
  error?: FieldError
  showErrors: boolean
  onChange: (value: number) => void
}

/** One labelled numeric input with an inline error message below it. */
function NumField({ label, value, min, max, step = 0.01, error, showErrors, onChange }: NumFieldProps) {
  const { t } = useTranslation()
  const invalid = showErrors && error !== undefined
  return (
    <div className="step-field">
      <span className="step-field-label">{label}</span>
      <InputNumber
        aria-label={label}
        value={Number.isNaN(value) ? null : value}
        min={min}
        max={max}
        step={step}
        status={invalid ? 'error' : undefined}
        onChange={(next) => onChange(typeof next === 'number' ? next : NaN)}
      />
      {invalid && error !== undefined && (
        <div className="step-field-error" role="alert">
          {errorMessage(t, error)}
        </div>
      )}
    </div>
  )
}

interface RowControlsProps {
  path: number[]
  index: number
  siblingCount: number
  level: number
  ops: Ops
}

/** Reorder / wrap-in-loop / delete controls shared by every row. */
function RowControls({ path, index, siblingCount, level, ops }: RowControlsProps) {
  const { t } = useTranslation()
  const canNest = level < MAX_NESTING_DEPTH && !ops.atMax
  return (
    <div className="step-controls">
      <Button
        size="small"
        icon={<ArrowUpOutlined />}
        aria-label={t('sequences.step.moveUp')}
        disabled={index === 0}
        onClick={() => ops.move(path, -1)}
      />
      <Button
        size="small"
        icon={<ArrowDownOutlined />}
        aria-label={t('sequences.step.moveDown')}
        disabled={index === siblingCount - 1}
        onClick={() => ops.move(path, 1)}
      />
      <Button
        size="small"
        icon={<GroupOutlined />}
        aria-label={t('sequences.step.wrapInLoop')}
        disabled={!canNest}
        onClick={() => ops.wrap(path)}
      />
      <Button
        size="small"
        danger
        icon={<DeleteOutlined />}
        aria-label={t('sequences.step.delete')}
        onClick={() => ops.remove(path)}
      />
    </div>
  )
}

interface AdvanceFieldsProps {
  condition: AutomationCondition
  errors: FieldError[]
  showErrors: boolean
  onChange: (condition: AutomationCondition) => void
}

/** The setHold advance condition sub-form (reuses the automation vocabulary). */
function AdvanceFields({ condition, errors, showErrors, onChange }: AdvanceFieldsProps) {
  const { t } = useTranslation()
  const typeOptions: Array<{ value: AutomationConditionType; label: string }> = [
    { value: 'currentBelow', label: t('automation.conditionTypes.currentBelow') },
    { value: 'capacityAbove', label: t('automation.conditionTypes.capacityAbove') },
    { value: 'energyAbove', label: t('automation.conditionTypes.energyAbove') },
    { value: 'elapsedAbove', label: t('automation.conditionTypes.elapsedAbove') },
  ]
  return (
    <div className="step-advance">
      <Typography.Text className="step-advance-title">
        {t('sequences.step.advanceWhen')}
      </Typography.Text>
      <div className="step-fields">
        <div className="step-field">
          <span className="step-field-label">{t('sequences.step.advanceType')}</span>
          <Select
            aria-label={t('sequences.step.advanceType')}
            value={condition.type}
            options={typeOptions}
            onChange={(type: AutomationConditionType) => onChange(conditionOfType(type))}
          />
        </div>
        {condition.type === 'currentBelow' && (
          <>
            <NumField
              label={t('automation.form.amps')}
              value={condition.amps}
              min={0}
              step={0.01}
              error={fieldError(errors, 'adv.amps')}
              showErrors={showErrors}
              onChange={(amps) => onChange({ type: 'currentBelow', amps, forSeconds: condition.forSeconds })}
            />
            <NumField
              label={t('automation.form.forSeconds')}
              value={condition.forSeconds}
              min={0}
              step={1}
              error={fieldError(errors, 'adv.forSeconds')}
              showErrors={showErrors}
              onChange={(forSeconds) => onChange({ type: 'currentBelow', amps: condition.amps, forSeconds })}
            />
          </>
        )}
        {condition.type === 'capacityAbove' && (
          <NumField
            label={t('automation.form.ah')}
            value={condition.ah}
            min={0}
            step={0.01}
            error={fieldError(errors, 'adv.ah')}
            showErrors={showErrors}
            onChange={(ah) => onChange({ type: 'capacityAbove', ah })}
          />
        )}
        {condition.type === 'energyAbove' && (
          <NumField
            label={t('automation.form.wh')}
            value={condition.wh}
            min={0}
            step={0.01}
            error={fieldError(errors, 'adv.wh')}
            showErrors={showErrors}
            onChange={(wh) => onChange({ type: 'energyAbove', wh })}
          />
        )}
        {condition.type === 'elapsedAbove' && (
          <NumField
            label={t('automation.form.seconds')}
            value={condition.seconds}
            min={0}
            step={1}
            error={fieldError(errors, 'adv.seconds')}
            showErrors={showErrors}
            onChange={(seconds) => onChange({ type: 'elapsedAbove', seconds })}
          />
        )}
      </div>
    </div>
  )
}

interface RowProps<T extends SequenceNode> {
  node: T
  path: number[]
  index: number
  siblingCount: number
  level: number
  showErrors: boolean
  ops: Ops
}

function SetHoldRow({ node, path, index, siblingCount, level, showErrors, ops }: RowProps<SetHoldNode>) {
  const { t } = useTranslation()
  const errors = nodeFieldErrors(node)
  const setField = (patch: Partial<SetHoldNode>) =>
    ops.update(path, (n) => ({ ...(n as SetHoldNode), ...patch }))
  return (
    <Card size="small">
      <Flex className="step-node-header" align="center" justify="space-between" wrap gap="small">
        <Tag icon={<AimOutlined />} color="blue">
          {t('sequences.step.setHold')}
        </Tag>
        <RowControls path={path} index={index} siblingCount={siblingCount} level={level} ops={ops} />
      </Flex>
      <div className="step-fields">
        <NumField
          label={t('sequences.form.volts')}
          value={node.volts}
          min={0}
          max={MAX_VOLTS}
          step={0.01}
          error={fieldError(errors, 'volts')}
          showErrors={showErrors}
          onChange={(volts) => setField({ volts })}
        />
        <NumField
          label={t('sequences.form.amps')}
          value={node.amps}
          min={0}
          max={MAX_AMPS}
          step={0.01}
          error={fieldError(errors, 'amps')}
          showErrors={showErrors}
          onChange={(amps) => setField({ amps })}
        />
      </div>
      <AdvanceFields
        condition={node.advance}
        errors={errors}
        showErrors={showErrors}
        onChange={(advance) => setField({ advance })}
      />
    </Card>
  )
}

function RampRow({ node, path, index, siblingCount, level, showErrors, ops }: RowProps<RampNode>) {
  const { t } = useTranslation()
  const errors = nodeFieldErrors(node)
  const max = node.target === 'current' ? MAX_AMPS : MAX_VOLTS
  const setField = (patch: Partial<RampNode>) =>
    ops.update(path, (n) => ({ ...(n as RampNode), ...patch }))
  const targetOptions: Array<{ value: RampTarget; label: string }> = [
    { value: 'voltage', label: t('sequences.form.targetVoltage') },
    { value: 'current', label: t('sequences.form.targetCurrent') },
  ]
  return (
    <Card size="small">
      <Flex className="step-node-header" align="center" justify="space-between" wrap gap="small">
        <Tag icon={<RiseOutlined />} color="geekblue">
          {t('sequences.step.ramp')}
        </Tag>
        <RowControls path={path} index={index} siblingCount={siblingCount} level={level} ops={ops} />
      </Flex>
      <div className="step-fields">
        <div className="step-field">
          <span className="step-field-label">{t('sequences.form.target')}</span>
          <Select
            aria-label={t('sequences.form.target')}
            value={node.target}
            options={targetOptions}
            onChange={(target: RampTarget) => setField({ target })}
          />
        </div>
        <NumField
          label={t('sequences.form.from')}
          value={node.from}
          min={0}
          max={max}
          step={0.01}
          error={fieldError(errors, 'from')}
          showErrors={showErrors}
          onChange={(from) => setField({ from })}
        />
        <NumField
          label={t('sequences.form.to')}
          value={node.to}
          min={0}
          max={max}
          step={0.01}
          error={fieldError(errors, 'to')}
          showErrors={showErrors}
          onChange={(to) => setField({ to })}
        />
        <NumField
          label={t('sequences.form.seconds')}
          value={node.seconds}
          min={0}
          step={0.1}
          error={fieldError(errors, 'seconds')}
          showErrors={showErrors}
          onChange={(seconds) => setField({ seconds })}
        />
      </div>
    </Card>
  )
}

function LoopRow({ node, path, index, siblingCount, level, showErrors, ops }: RowProps<LoopNode>) {
  const { t } = useTranslation()
  const errors = nodeFieldErrors(node)
  const setField = (patch: Partial<LoopNode>) =>
    ops.update(path, (n) => ({ ...(n as LoopNode), ...patch }))
  const emptyChildren = node.children.length === 0
  return (
    <Card size="small">
      <Flex className="step-node-header" align="center" justify="space-between" wrap gap="small">
        <Flex align="center" gap="small" wrap>
          <Tag icon={<RetweetOutlined />} color="purple">
            {t('sequences.step.loop')}
          </Tag>
          <div className="step-field" style={{ flex: '0 0 auto', minWidth: 96 }}>
            <span className="step-field-label">{t('sequences.form.repeat')}</span>
            <InputNumber
              aria-label={t('sequences.form.repeat')}
              value={Number.isNaN(node.repeat) ? null : node.repeat}
              min={1}
              step={1}
              status={showErrors && fieldError(errors, 'repeat') !== undefined ? 'error' : undefined}
              onChange={(next) => setField({ repeat: typeof next === 'number' ? next : NaN })}
            />
          </div>
        </Flex>
        <RowControls path={path} index={index} siblingCount={siblingCount} level={level} ops={ops} />
      </Flex>
      {showErrors && fieldError(errors, 'repeat') !== undefined && (
        <div className="step-field-error" role="alert">
          {errorMessage(t, fieldError(errors, 'repeat') as FieldError)}
        </div>
      )}
      {showErrors && emptyChildren && (
        <div className="step-field-error" role="alert">
          {t('sequences.form.loopEmpty')}
        </div>
      )}
      <div className="step-children">
        <NodeList
          nodes={node.children}
          parentPath={path}
          level={level + 1}
          showErrors={showErrors}
          ops={ops}
        />
      </div>
    </Card>
  )
}

interface AddToolbarProps {
  parentPath: number[]
  level: number
  ops: Ops
}

function AddToolbar({ parentPath, level, ops }: AddToolbarProps) {
  const { t } = useTranslation()
  const canNest = level < MAX_NESTING_DEPTH && !ops.atMax
  return (
    <Flex className="step-add-toolbar" wrap gap="small">
      <Button
        size="small"
        icon={<PlusOutlined />}
        disabled={ops.atMax}
        onClick={() => ops.add(parentPath, 'setHold')}
      >
        {t('sequences.step.addSetHold')}
      </Button>
      <Button
        size="small"
        icon={<PlusOutlined />}
        disabled={ops.atMax}
        onClick={() => ops.add(parentPath, 'ramp')}
      >
        {t('sequences.step.addRamp')}
      </Button>
      <Button
        size="small"
        icon={<PlusOutlined />}
        disabled={!canNest}
        onClick={() => ops.add(parentPath, 'loop')}
      >
        {t('sequences.step.addLoop')}
      </Button>
    </Flex>
  )
}

interface NodeListProps {
  nodes: SequenceNode[]
  parentPath: number[]
  level: number
  showErrors: boolean
  ops: Ops
}

function NodeList({ nodes, parentPath, level, showErrors, ops }: NodeListProps) {
  return (
    <div className="step-list">
      {nodes.map((node, i) => {
        const path = [...parentPath, i]
        const common = {
          path,
          index: i,
          siblingCount: nodes.length,
          level,
          showErrors,
          ops,
        }
        // Positional key: reordering is via buttons only, not drag, so a
        // stable-by-index key is fine for this controlled outline.
        switch (node.type) {
          case 'setHold':
            return <SetHoldRow key={i} node={node} {...common} />
          case 'ramp':
            return <RampRow key={i} node={node} {...common} />
          case 'loop':
            return <LoopRow key={i} node={node} {...common} />
        }
      })}
      <AddToolbar parentPath={parentPath} level={level} ops={ops} />
    </div>
  )
}

export interface StepTreeEditorProps {
  value: SequenceNode[]
  onChange: (steps: SequenceNode[]) => void
  /** Flip true once the user has attempted an invalid submit. */
  showErrors: boolean
}

/**
 * Outline editor for a Program's step tree (F-022): an indented list of
 * SetHold / Ramp / Loop rows with inline parameter editing, add / wrap-in-loop /
 * reorder / delete controls, and inline validation surfaced when `showErrors`.
 */
export function StepTreeEditor({ value, onChange, showErrors }: StepTreeEditorProps) {
  const { t } = useTranslation()
  const issues = programIssues(value)
  const ops: Ops = {
    update: (path, updater) => onChange(updateNodeAt(value, path, updater)),
    remove: (path) => onChange(removeNodeAt(value, path)),
    move: (path, dir) => onChange(moveNodeAt(value, path, dir)),
    wrap: (path) => onChange(wrapNodeInLoop(value, path)),
    add: (parentPath, type) =>
      onChange(
        parentPath.length === 0
          ? appendTopLevel(value, newNode(type))
          : appendChildAt(value, parentPath, newNode(type)),
      ),
    atMax: countNodes(value) >= MAX_NODE_COUNT,
  }

  return (
    <div>
      {showErrors && issues.tooDeep && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={t('sequences.form.tooDeep', { max: MAX_NESTING_DEPTH })}
        />
      )}
      {showErrors && issues.tooMany && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message={t('sequences.form.tooMany', { max: MAX_NODE_COUNT })}
        />
      )}
      {value.length === 0 && (
        <Typography.Paragraph type="secondary">
          {t('sequences.editor.emptyHint')}
        </Typography.Paragraph>
      )}
      <NodeList nodes={value} parentPath={[]} level={1} showErrors={showErrors} ops={ops} />
    </div>
  )
}
