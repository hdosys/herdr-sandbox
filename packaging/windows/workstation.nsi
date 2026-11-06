Unicode true
RequestExecutionLevel admin
ManifestDPIAware true
SetCompressor /SOLID lzma
SetCompressorDictSize 32
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
Var ProfileControl
Var RefreshControl
Var RefreshArg
Var ExportPath
Var SettingsArg
Var DefaultsArg
Var FinishText
Var SetupExit
Var StateRoot
Var WasSilent

!define MUI_WELCOMEPAGE_TITLE "${APP_DISPLAY_NAME} ${BUILD_FRESHNESS}"
!define MUI_WELCOMEPAGE_TEXT "Set up this Windows 10/11 x64 computer directly, without creating a Sandbox. Windows 11 is recommended.$\r$\n$\r$\nInternet access and administrator rights are required. The full development selection can take several hours. Independent steps continue after failures; each step has a deadline.$\r$\n$\r$\nRe-run this installer to resume. Accounts and licences require separate sign-in. Existing Herdr sessions and Windows security policy are not replaced."
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\LICENSE.txt"
Page custom SelectProfile LeaveProfile
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${BUILD_FRESHNESS}"
!define MUI_FINISHPAGE_TEXT "$FinishText"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Open the setup report"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenReport
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
  ${GetOptions} $0 "/PROFILE=" $ProfilePath
  ClearErrors
  ${GetOptions} $0 "/ROOT=" $StateRoot
  ClearErrors
  ${GetOptions} $0 "/EXPORTPROFILE=" $ExportPath
  ClearErrors
  ${GetOptions} $0 "/REFRESH" $1
  ${IfNot} ${Errors}
    StrCpy $RefreshArg "--refresh"
  ${EndIf}
  ClearErrors
  ${GetOptions} $0 "/INCLUDESETTINGS" $1
  ${IfNot} ${Errors}
    StrCpy $SettingsArg "--include-settings"
  ${EndIf}
  ClearErrors
  ${GetOptions} $0 "/DEFAULTS" $1
  ${IfNot} ${Errors}
    StrCpy $DefaultsArg "--defaults"
  ${EndIf}
  ClearErrors
FunctionEnd

Function SelectProfile
  !insertmacro MUI_HEADER_TEXT "Configuration and resume" "Choose a private profile or resume this computer's saved selection."
  nsDialogs::Create 1018
  Pop $0
  ${NSD_CreateLabel} 0 0 100% 42u "Optional: select the private profile exported on your previous computer. Otherwise setup resumes its saved selection, or uses the full development selection on a new machine. Existing different configuration is preserved."
  Pop $0
  ${NSD_CreateFileRequest} 0 48u 84% 13u "$ProfilePath"
  Pop $ProfileControl
  ${NSD_CreateBrowseButton} 86% 47u 14% 15u "Browse..."
  Pop $0
  ${NSD_OnClick} $0 BrowseProfile
  ${NSD_CreateCheckbox} 0 75u 100% 14u "Refresh completed steps instead of resuming unfinished work"
  Pop $RefreshControl
  ${If} $RefreshArg != ""
    ${NSD_Check} $RefreshControl
  ${EndIf}
  ${NSD_CreateLabel} 0 100u 100% 40u "Only use a profile you trust. Authored configuration can contain commands or private values. Dedicated authentication files, SSH keys and project data are excluded. Keep the original private profile available for configuration repair."
  Pop $0
  nsDialogs::Show
FunctionEnd

Function BrowseProfile
  Pop $0
  nsDialogs::SelectFileDialog open "" "Workstation profile (*.json)|*.json"
  Pop $0
  ${If} $0 != ""
    ${NSD_SetText} $ProfileControl $0
  ${EndIf}
FunctionEnd

Function LeaveProfile
  ${NSD_GetText} $ProfileControl $ProfilePath
  ${NSD_GetState} $RefreshControl $0
  StrCpy $RefreshArg ""
  ${If} $0 == ${BST_CHECKED}
    StrCpy $RefreshArg "--refresh"
  ${EndIf}
FunctionEnd

Section "Workstation setup"
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
  ${If} $ExportPath != ""
    DetailPrint "Exporting the explicitly selected configuration. No tools will be installed."
    nsExec::ExecToLog '$\"$PLUGINSDIR\payload\sandbox.exe$\" workstation export --output $\"$ExportPath$\" $SettingsArg $DefaultsArg'
    Pop $SetupExit
    StrCpy $FinishText "Profile export finished with exit status $SetupExit. No tool provisioning was run. If successful, transfer the private profile separately and select it when running this installer on the new computer."
    ${If} $SetupExit != "0"
      SetErrorLevel 1
    ${EndIf}
    Goto setup_finished
  ${EndIf}
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
    nsExec::ExecToLog '$\"$PLUGINSDIR\payload\sandbox.exe$\" workstation setup --root $\"$StateRoot$\" $RefreshArg'
  ${Else}
    nsExec::ExecToLog '$\"$PLUGINSDIR\payload\sandbox.exe$\" workstation setup --profile $\"$ProfilePath$\" --root $\"$StateRoot$\" $RefreshArg'
  ${EndIf}
  Pop $SetupExit
  ${If} $SetupExit == "0"
    StrCpy $FinishText "Setup completed. The report contains the checked component results. Accounts and licensed applications still require your separate sign-in. Re-run this installer to resume or refresh later."
  ${Else}
    StrCpy $FinishText "Setup finished with incomplete work (exit $SetupExit). Open the report for failed, blocked or interrupted steps, then run this installer again to resume. Exit 2 means setup could not start safely and no current report is claimed."
    SetErrorLevel 1
  ${EndIf}
  setup_finished:
SectionEnd

Function OpenReport
  ${If} $SetupExit == "0"
  ${OrIf} $SetupExit == "1"
    ${If} $ExportPath == ""
    ${AndIf} ${FileExists} "$StateRoot\report.html"
      ExecShell "open" "$StateRoot\report.html"
    ${EndIf}
  ${EndIf}
FunctionEnd
