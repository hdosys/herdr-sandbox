Unicode true
RequestExecutionLevel admin
ManifestDPIAware true
; Downloaded packages and toolchain inputs are already compressed.
; Do not spend build or installation time recompressing them.
SetCompress off
AllowSkipFiles off
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "TextFunc.nsh"

Name "${APP_DISPLAY_NAME}"
Caption "${APP_DISPLAY_NAME} Setup"
OutFile "${OUTPUT_FILE}"
BrandingText "${APP_DISPLAY_NAME}"
VIProductVersion "${FIXED_VERSION}"
VIAddVersionKey /LANG=1033 "ProductName" "${APP_DISPLAY_NAME}"
VIAddVersionKey /LANG=1033 "ProductVersion" "${BUILD_FRESHNESS}"
VIAddVersionKey /LANG=1033 "FileVersion" "${BUILD_FRESHNESS}"
VIAddVersionKey /LANG=1033 "FileDescription" "Direct Windows development setup"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Copyright (c) 2026 hdosys"

Var ProfilePath
Var RefreshControl
Var RefreshArg
Var FinishText
Var SetupExit
Var StateRoot
Var WasSilent
Var Passive

!define MUI_WELCOMEPAGE_TITLE "${APP_DISPLAY_NAME} ${BUILD_FRESHNESS}"
!define MUI_WELCOMEPAGE_TEXT "This PRIVATE installer contains selected configuration and supported authentication captured on the source computer. Possession may grant access to the selected accounts. No source-computer or SSH connection is required.$\r$\n$\r$\nWindows 10/11 x64 and administrator rights are required. Independent steps continue after failures.$\r$\n$\r$\nNever publish this installer. Existing Herdr sessions, Windows protections and audio functionality are preserved."
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipPassivePage
!insertmacro MUI_PAGE_WELCOME
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipPassivePage
!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\LICENSE.txt"
Page custom SelectProfile LeaveProfile
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${BUILD_FRESHNESS}"
!define MUI_FINISHPAGE_TEXT "$FinishText"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Open the setup report"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenReport
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipPassivePage
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${RunningX64}
    SetErrorLevel 2
    Abort "This setup requires Windows x64."
  ${EndIf}
  InitPluginsDir
  SetShellVarContext current
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/PASSIVE" $1
  ${IfNot} ${Errors}
    StrCpy $Passive 1
  ${EndIf}
  ClearErrors
  ${GetOptions} $0 "/PROFILE=" $ProfilePath
  ClearErrors
  ${GetOptions} $0 "/ROOT=" $StateRoot
  ClearErrors
  ${GetOptions} $0 "/REFRESH" $1
  ${IfNot} ${Errors}
    StrCpy $RefreshArg "--refresh"
  ${EndIf}
  ClearErrors
FunctionEnd

Function SkipPassivePage
  ${If} $Passive == 1
    Abort
  ${EndIf}
FunctionEnd

Function SelectProfile
  Call SkipPassivePage
  !insertmacro MUI_HEADER_TEXT "Your configuration is included" "Apply the snapshot embedded in this private installer."
  nsDialogs::Create 1018
  Pop $0
  ${NSD_CreateLabel} 0 0 100% 42u "The selected programs, stacks and personal settings are already in this installer. Setup imports them automatically. Existing different configuration is preserved and reported as a conflict, never silently overwritten."
  Pop $0
  ${NSD_CreateCheckbox} 0 75u 100% 14u "Refresh completed steps instead of resuming unfinished work"
  Pop $RefreshControl
  ${If} $RefreshArg != ""
    ${NSD_Check} $RefreshControl
  ${EndIf}
  ${NSD_CreateLabel} 0 100u 100% 40u "Personal settings may contain commands and inline private values. Keep this installer private. Dedicated authentication files, SSH keys, project data and Git history are excluded. Create a new named installation on the source computer, then build it to capture later changes."
  Pop $0
  nsDialogs::Show
FunctionEnd

Function LeaveProfile
  ${NSD_GetState} $RefreshControl $0
  StrCpy $RefreshArg ""
  ${If} $0 == ${BST_CHECKED}
    StrCpy $RefreshArg "--refresh"
  ${EndIf}
FunctionEnd

Section "Workstation setup"
  ${If} $Passive == 1
    SetAutoClose true
  ${EndIf}
  SetDetailsView show
  SetOutPath "$PLUGINSDIR\payload"
  StrCpy $WasSilent 1
  IfSilent extract_payload
  StrCpy $WasSilent 0
  SetSilent silent
  extract_payload:
  ClearErrors
  File "${PACKAGE_DIR}\sandbox.exe"
  File "${PACKAGE_DIR}\base.ps1"
  File "${PACKAGE_DIR}\stacks.ps1"
  File "${PACKAGE_DIR}\LICENSE.txt"
  File "${PACKAGE_DIR}\personal-profile.json"
  ${If} $WasSilent == 0
    SetSilent normal
  ${EndIf}
  ${If} ${Errors}
    StrCpy $FinishText "Setup files could not be extracted. No provisioning was run. Check disk space and permissions, then run this installer again."
    SetErrorLevel 2
    Goto setup_finished
  ${EndIf}
  GetDlgItem $0 $HWNDPARENT 2
  EnableWindow $0 0
  ${If} $StateRoot == ""
    nsExec::ExecToStack '$\"$PLUGINSDIR\payload\sandbox.exe$\" workstation root'
    Pop $SetupExit
    Pop $StateRoot
    ${TrimNewLines} $StateRoot $StateRoot
    ${If} $SetupExit != "0"
      StrCpy $FinishText "The intended Windows user or setup-state directory could not be resolved. No provisioning was run."
      SetErrorLevel 2
      Goto setup_finished
    ${EndIf}
  ${EndIf}
  DetailPrint "Running bounded setup. Independent steps continue after errors."
  ${If} $ProfilePath == ""
    StrCpy $ProfilePath "$PLUGINSDIR\payload\personal-profile.json"
  ${EndIf}
  nsExec::ExecToLog '$\"$PLUGINSDIR\payload\sandbox.exe$\" workstation setup --profile $\"$ProfilePath$\" --root $\"$StateRoot$\" $RefreshArg'
  Pop $SetupExit
  ${If} $SetupExit == "0"
    StrCpy $FinishText "Setup completed. The report contains the checked component results. Accounts and licensed applications still require your separate sign-in. Re-run this installer to resume or refresh later."
  ${Else}
    StrCpy $FinishText "Setup finished with incomplete work (exit $SetupExit). Open the report for failed, blocked or interrupted steps, then run this installer again to resume. Exit 2 means no current report was published; some work may have completed."
    SetErrorLevel 1
  ${EndIf}
  setup_finished:
SectionEnd

Function OpenReport
  ${If} $SetupExit == "0"
  ${OrIf} $SetupExit == "1"
    ${If} ${FileExists} "$StateRoot\report.html"
      ExecShell "open" "$StateRoot\report.html"
    ${EndIf}
  ${EndIf}
FunctionEnd
