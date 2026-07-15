import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import userRiskControl from './userRiskControl'
import accountMonitor from './accountMonitor'

export default {
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...userRiskControl,
  ...accountMonitor,
}
