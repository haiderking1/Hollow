// PORT: backend/workflow/types.go

export const DefaultMaxConcurrency = 16;
export const DefaultMaxTotalAgents = 1000;

export interface Meta {
  name: string;
  description: string;
  phases?: string[];
  maxConcurrency?: number;
  maxTotalAgents?: number;
}

export interface AgentOptions {
  key?: string;
  role: string;
  prompt: string;
  systemPrompt?: string;
  tools?: string[];
  model?: string;
  responseSchema?: Record<string, any>;
  maxTurns?: number;
  readonly?: boolean;
}

export interface AgentResult {
  key?: string;
  role?: string;
  ok: boolean;
  text: string;
  json?: any;
  error?: string;
  tokensUsed?: number;
  turnCount?: number;
}

export interface PipelineResult {
  input?: any;
  stages: StageResult[];
  results: Record<string, AgentResult>;
}

export interface StageResult {
  name: string;
  results: AgentResult[];
}

export interface BashResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  truncated?: boolean;
  fullOutputPath?: string;
  sha256?: string;
}

export interface AgentSnapshot {
  key: string;
  phase: string;
  role: string;
  status: string;
  prompt: string;
  lastTools?: string[];
  result?: string;
  json?: any;
  error?: string;
  tokens?: number;
  turns?: number;
  startedAt?: Date;
  endedAt?: Date;
}

export interface PhaseSnapshot {
  name: string;
  total: number;
  queued: number;
  running: number;
  done: number;
  failed: number;
  tokens: number;
}

export interface Snapshot {
  id: string;
  name: string;
  description: string;
  scriptPath: string;
  status: string;
  phase: string;
  phases: PhaseSnapshot[];
  agents: Record<string, AgentSnapshot>;
  queued: number;
  running: number;
  done: number;
  failed: number;
  tokens: number;
  startedAt: Date;
  endedAt?: Date;
  message?: string;
}

export interface RunOptions {
  ID: string;
  Args: string;
  Force: boolean;
}

export interface RunResult {
  id: string;
  meta: Meta;
  value?: any;
  status: string;
}

export function cloneJSON<T>(v: T): T {
  if (v === undefined) return undefined as any;
  return JSON.parse(JSON.stringify(v));
}

/*
PORT STATUS
source path: backend/workflow/types.go
source lines: 128
draft lines: 130
confidence: high
status: phase_b_compile
*/
