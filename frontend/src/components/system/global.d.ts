import type * as Controls from './controls'
import type * as Composite from './composite'
import type * as Presentation from './presentation'
import type * as Tables from './table'
import type * as Overlays from './overlays'

declare module 'vue' {
  export interface GlobalComponents {
    UiButton: typeof Controls.UiButton
    UiInput: typeof Controls.UiInput
    UiInputNumber: typeof Controls.UiInputNumber
    UiForm: typeof Controls.UiForm
    UiFormItem: typeof Controls.UiFormItem
    UiTag: typeof Controls.UiTag
    UiSwitch: typeof Controls.UiSwitch
    UiDatePicker: typeof Controls.UiDatePicker
    UiSelect: typeof Composite.UiSelect
    UiOption: typeof Composite.UiOption
    UiTabs: typeof Composite.UiTabs
    UiTabPane: typeof Composite.UiTabPane
    UiRadioGroup: typeof Composite.UiRadioGroup
    UiRadio: typeof Composite.UiRadio
    UiRadioButton: typeof Composite.UiRadioButton
    UiCheckbox: typeof Composite.UiCheckbox
    UiUpload: typeof Composite.UiUpload
    UiPagination: typeof Composite.UiPagination
    UiTable: typeof Tables.UiTable
    UiTableColumn: typeof Tables.UiTableColumn
    UiDialog: typeof Overlays.UiDialog
    UiDrawer: typeof Overlays.UiDrawer
    UiTooltip: typeof Overlays.UiTooltip
    UiCard: typeof Presentation.UiCard
    UiAlert: typeof Presentation.UiAlert
    UiEmpty: typeof Presentation.UiEmpty
    UiResult: typeof Presentation.UiResult
    UiSkeleton: typeof Presentation.UiSkeleton
    UiProgress: typeof Presentation.UiProgress
    UiLink: typeof Presentation.UiLink
    UiDivider: typeof Presentation.UiDivider
    UiBadge: typeof Presentation.UiBadge
    UiAvatar: typeof Presentation.UiAvatar
    UiDescriptions: typeof Presentation.UiDescriptions
    UiDescriptionsItem: typeof Presentation.UiDescriptionsItem
    UiCollapse: typeof Presentation.UiCollapse
    UiCollapseItem: typeof Presentation.UiCollapseItem
    UiCollapseTransition: typeof Presentation.UiCollapseTransition
  }
}
export {}
