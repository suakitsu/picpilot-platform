'use client'

import { Check } from 'lucide-react'

type TaskFlowStepsProps = {
  steps: string[]
  currentStep: number
  ariaLabel: string
  onStepChange: (step: number) => void
}

export function TaskFlowSteps({ steps, currentStep, ariaLabel, onStepChange }: TaskFlowStepsProps) {
  return (
    <nav className="task-flow-steps" aria-label={ariaLabel}>
      {steps.map((label, index) => (
        <button
          key={label}
          type="button"
          className={currentStep === index ? 'active' : currentStep > index ? 'done' : ''}
          aria-current={currentStep === index ? 'step' : undefined}
          onClick={() => onStepChange(index)}
        >
          <span>{currentStep > index ? <Check size={13} /> : index + 1}</span>
          <strong>{label}</strong>
        </button>
      ))}
    </nav>
  )
}
