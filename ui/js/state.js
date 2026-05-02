export const state = {
  servers: [],
  agents: [],
  gameDefs: [],
  selectedServer: null,
  selectedAgent: null,
  ws: null,
  wsConnected: false,
  pendingRestart: null,
  agentMetricsHistory: {},
  containerMetricsHistory: {},
}
