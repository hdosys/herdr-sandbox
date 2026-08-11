Unicode true

!ifndef RELEASE_TAG
    !error "RELEASE_TAG is required"
!endif
!ifndef VERSION
    !error "VERSION is required"
!endif
!ifndef FIXED_VERSION
    !error "FIXED_VERSION is required"
!endif
!ifndef PACKAGE_DIR
    !error "PACKAGE_DIR is required"
!endif
!ifndef PATH_HELPER
    !error "PATH_HELPER is required"
!endif
!ifndef QUIET_UNINSTALL_HELPER
    !error "QUIET_UNINSTALL_HELPER is required"
!endif
!ifndef OUTPUT_FILE
    !error "OUTPUT_FILE is required"
!endif
!ifndef OUTPUT_FILE_NAME
    !error "OUTPUT_FILE_NAME is required"
!endif
!ifndef APP_NAME
    !error "APP_NAME is required"
!endif
!ifndef APP_DISPLAY_NAME
    !error "APP_DISPLAY_NAME is required"
!endif
!ifndef APP_EXECUTABLE
    !error "APP_EXECUTABLE is required"
!endif
!ifndef APP_REPLACED_EXECUTABLE
    !error "APP_REPLACED_EXECUTABLE is required"
!endif
!ifndef APP_BASE_SCRIPT
    !error "APP_BASE_SCRIPT is required"
!endif
!ifndef APP_STACK_SCRIPT
    !error "APP_STACK_SCRIPT is required"
!endif
!ifndef APP_LICENSE
    !error "APP_LICENSE is required"
!endif
!ifndef APP_CONFIG_FILE
    !error "APP_CONFIG_FILE is required"
!endif
!ifndef APP_USER_SCRIPT
    !error "APP_USER_SCRIPT is required"
!endif
!ifndef APP_PROJECT_DIRECTORY
    !error "APP_PROJECT_DIRECTORY is required"
!endif
!ifndef APP_INSTALL_DIRECTORY
    !error "APP_INSTALL_DIRECTORY is required"
!endif
!ifndef APP_PUBLISHER
    !error "APP_PUBLISHER is required"
!endif
!ifndef APP_PRODUCT_URL
    !error "APP_PRODUCT_URL is required"
!endif
!ifndef APP_PRODUCT_GUID
    !error "APP_PRODUCT_GUID is required"
!endif
!ifndef APP_UNINSTALL_KEY
    !error "APP_UNINSTALL_KEY is required"
!endif
!ifndef APP_INSTALLER_MARKER
    !error "APP_INSTALLER_MARKER is required"
!endif
!ifndef APP_QUIET_UNINSTALL_HELPER
    !error "APP_QUIET_UNINSTALL_HELPER is required"
!endif
!ifndef APP_COPYRIGHT
    !error "APP_COPYRIGHT is required"
!endif

!if "${RELEASE_TAG}" == ""
    !error "RELEASE_TAG must not be empty"
!endif
!if "${VERSION}" == ""
    !error "VERSION must not be empty"
!endif
!if "${FIXED_VERSION}" == ""
    !error "FIXED_VERSION must not be empty"
!endif
!if "${APP_PRODUCT_GUID}" == ""
    !error "APP_PRODUCT_GUID must not be empty"
!endif
!if "${APP_UNINSTALL_KEY}" == ""
    !error "APP_UNINSTALL_KEY must not be empty"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "WinVer.nsh"
!include "x64.nsh"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_KEY}"
!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 100
!define APP_LIFECYCLE_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerLifecycle.v3"
!define APP_ERROR_FILE_NOT_FOUND 2
!define APP_ERROR_PATH_NOT_FOUND 3
!define APP_WAIT_OBJECT_0 0
!define APP_WAIT_ABANDONED 128
!define APP_WAIT_TIMEOUT 258
!define APP_FILE_ATTRIBUTE_DIRECTORY 0x10
!define APP_FILE_ATTRIBUTE_REPARSE_POINT 0x400
!define APP_DELETE_ACCESS 0x00010000
!define APP_FILE_SHARE_ALL 0x7
!define APP_OPEN_EXISTING 3
!define APP_EXIT_INVALID_ARGUMENTS 30
!define APP_EXIT_LIFECYCLE_BUSY 41
!define APP_EXIT_UNSUPPORTED_PLATFORM 50
!define APP_EXIT_PAYLOAD_FAILURE 60
!define APP_EXIT_INSTALL_FAILED 70
!define APP_EXIT_INSTALL_ROLLBACK_INCOMPLETE 71
!define APP_EXIT_INSTALL_INTEGRATION 72
!define APP_EXIT_UNINSTALL_PREFLIGHT 80
!define APP_EXIT_UNINSTALL_FINALIZE 81
!define APP_EXIT_UNINSTALL_CLEANUP 82
!define APP_EXIT_UNINSTALL_PARTIAL_CLEANUP 83
!define APP_EXIT_INTERNAL_STATE 90

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var InstallerLifecycleMutexHandle
Var EnvironmentNotificationFailed
Var InstallMutationActive
Var ExistingRegistrationOwned
Var PathOwnedPreviously
Var PathPresentBefore
Var PathAddedThisRun
Var PathAddPending
Var InstallCompleteWasComplete
Var OwnershipMarkerValid
Var InstallDirectorySafe
Var BackupBaseScript
Var BackupLicense
Var BackupStackScript
Var BackupQuietUninstall
Var BackupUninstaller
Var BackupExecutable
Var BackupReplacedExecutable
Var BackupMarker
Var BackupFailed
Var PayloadCopyFailed
Var RollbackFailed
Var InstallFailureMessage
Var CleanupComplete
Var UninstallPreflightFailed
Var PartialCleanup
Var InstallDirectoryHasUnknownEntries

Name "${APP_DISPLAY_NAME}"
OutFile "${OUTPUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
RequestExecutionLevel user
CRCCheck force
SetCompressor lzma
SetDatablockOptimize on
SetCompressorDictSize 8
SetCompressor /SOLID /FINAL lzma
AllowSkipFiles off
ManifestDPIAware true
ShowInstDetails show
AutoCloseWindow true

VIProductVersion "${FIXED_VERSION}"
VIFileVersion "${FIXED_VERSION}"
VIAddVersionKey "ProductName" "${APP_DISPLAY_NAME}"
VIAddVersionKey "FileDescription" "${APP_DISPLAY_NAME} Installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey "LegalCopyright" "${APP_COPYRIGHT}"
VIAddVersionKey "OriginalFilename" "${OUTPUT_FILE_NAME}"

!define MUI_ABORTWARNING
!define MUI_CUSTOMFUNCTION_ABORT PreventInstallMutationAbort
!define MUI_CUSTOMFUNCTION_UNABORT un.PreventUninstallMutationAbort
!define INSTALLER_WELCOME_BITMAP_100 "${__FILEDIR__}\assets\installer-welcome-finish-164x314.bmp"
!define INSTALLER_WELCOME_BITMAP_125 "${__FILEDIR__}\assets\installer-welcome-finish-205x393.bmp"
!define INSTALLER_WELCOME_BITMAP_150 "${__FILEDIR__}\assets\installer-welcome-finish-246x471.bmp"
!define INSTALLER_WELCOME_BITMAP_175 "${__FILEDIR__}\assets\installer-welcome-finish-287x550.bmp"
!define INSTALLER_WELCOME_BITMAP_200 "${__FILEDIR__}\assets\installer-welcome-finish-328x628.bmp"
!define MUI_WELCOMEFINISHPAGE_BITMAP "${INSTALLER_WELCOME_BITMAP_100}"
!define MUI_WELCOMEFINISHPAGE_BITMAP_STRETCH NoStretchNoCropNoAlign
!define MUI_CUSTOMFUNCTION_GUIINIT SelectInstallerWelcomeBitmap
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_125}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_150}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_175}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_200}"
!define MUI_WELCOMEPAGE_TITLE "Install ${APP_DISPLAY_NAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "This setup installs ${APP_DISPLAY_NAME} for your Windows account and creates its default configuration when missing.$\r$\n$\r$\nNo administrator access is required. Open a new terminal after setup so it can find ${APP_NAME} on PATH."
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!define MUI_FINISHPAGE_TEXT_LARGE
!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${VERSION} is installed"
!define MUI_FINISHPAGE_TEXT "Setup completed successfully.$\r$\n$\r$\nOpen a new terminal in a project directory. Run ${APP_NAME} init for a new project or ${APP_NAME} up for an existing profile."
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Open ${APP_DISPLAY_NAME} configuration"
!define MUI_FINISHPAGE_RUN_FUNCTION OpenInstalledConfiguration
!define MUI_FINISHPAGE_LINK "Open setup and usage guide"
!define MUI_FINISHPAGE_LINK_LOCATION "${APP_PRODUCT_URL}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"
!insertmacro MUI_PAGE_INSTFILES
!define MUI_PAGE_CUSTOMFUNCTION_SHOW ConfigureInstallerFinishPage
!insertmacro MUI_PAGE_FINISH
UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function SelectInstallerWelcomeBitmap
    ; Resolve dynamically so unsupported hosts fail to the 96-DPI asset rather
    ; than failing GUI initialization before the Windows-version message.
    System::Call 'KERNEL32::GetModuleHandleW(w "USER32.DLL") p.r0'
    System::Call 'KERNEL32::GetProcAddress(p r0, m "GetDpiForWindow") p.r1'
    ${If} $1 == 0
        StrCpy $0 96
    ${Else}
        System::Call '::$1(p $HWNDPARENT)i.r0'
        ${If} $0 == 0
            StrCpy $0 96
        ${EndIf}
    ${EndIf}
    ${If} $0 >= 180
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_200}"
    ${ElseIf} $0 >= 156
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_175}"
    ${ElseIf} $0 >= 132
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_150}"
    ${ElseIf} $0 >= 108
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_125}"
    ${EndIf}
FunctionEnd

Function ConfigureInstallerFinishPage
    ${If} $ExistingRegistrationOwned != "0"
        ${NSD_Uncheck} $mui.FinishPage.Run
        ShowWindow $mui.FinishPage.Run ${SW_HIDE}
        ${NSD_SetFocus} $mui.Button.Next
    ${EndIf}
FunctionEnd

Function OpenInstalledConfiguration
    IfSilent open_configuration_done
    ${If} $ExistingRegistrationOwned != "0"
        Goto open_configuration_done
    ${EndIf}
    nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-open-configuration'
    Pop $0
    Pop $1
    ${If} $0 == "error"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but Windows could not start configuration opening. Run ${APP_NAME} config from a new terminal." /SD IDOK
    ${ElseIf} $0 != "0"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but its configuration could not be opened: application status $0. $1 Run ${APP_NAME} config from a new terminal." /SD IDOK
    ${EndIf}
    open_configuration_done:
FunctionEnd

!macro AcquireInstallerLifecycleMutex
    ; The mutex is acquired, not inferred from object existence. Windows grants
    ; an abandoned mutex to the next waiter after a process or system crash.
    System::Call 'KERNEL32::CreateMutexW(p 0, i 0, w "${APP_LIFECYCLE_MUTEX_NAME}") p.r1'
    StrCpy $InstallerLifecycleMutexHandle $1
    ${If} $1 == 0
        MessageBox MB_ICONSTOP|MB_OK "Windows could not create the ${APP_DISPLAY_NAME} installer lifecycle gate. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
        Quit
    ${EndIf}
    System::Call 'KERNEL32::WaitForSingleObject(p $1, i 0) i.r0'
    ${If} $0 == ${APP_WAIT_OBJECT_0}
    ${OrIf} $0 == ${APP_WAIT_ABANDONED}
        ; This thread now owns the mutex and holds it until terminal process exit.
        Nop
    ${ElseIf} $0 == ${APP_WAIT_TIMEOUT}
        System::Call 'KERNEL32::CloseHandle(p $1)'
        StrCpy $InstallerLifecycleMutexHandle 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "Another ${APP_DISPLAY_NAME} setup, uninstall, or sandbox command is currently running. Close that live command, then run setup again." /SD IDOK
        SetErrorLevel ${APP_EXIT_LIFECYCLE_BUSY}
        Quit
    ${Else}
        System::Call 'KERNEL32::CloseHandle(p $1)'
        StrCpy $InstallerLifecycleMutexHandle 0
        MessageBox MB_ICONSTOP|MB_OK "Windows could not acquire the ${APP_DISPLAY_NAME} installer lifecycle gate. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
        Quit
    ${EndIf}
!macroend

!macro ReleaseInstallerLifecycleMutex
    ${If} $InstallerLifecycleMutexHandle != 0
        System::Call 'KERNEL32::ReleaseMutex(p $InstallerLifecycleMutexHandle) i.r0'
        System::Call 'KERNEL32::CloseHandle(p $InstallerLifecycleMutexHandle)'
        StrCpy $InstallerLifecycleMutexHandle 0
    ${EndIf}
!macroend

Function PreventInstallMutationAbort
    ${If} $InstallMutationActive == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is replacing or restoring its installed files. Wait for setup to finish." /SD IDOK
        Abort
    ${EndIf}
FunctionEnd

Function un.PreventUninstallMutationAbort
    ${If} $InstallMutationActive == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is cleaning or removing installed state. Wait for uninstall to finish." /SD IDOK
        Abort
    ${EndIf}
FunctionEnd

Function DisableInstallCancellation
    IfSilent done
    GetDlgItem $0 $HWNDPARENT 2
    EnableWindow $0 0
    done:
FunctionEnd

Function EnableInstallCancellation
    IfSilent done
    GetDlgItem $0 $HWNDPARENT 2
    EnableWindow $0 1
    done:
FunctionEnd

Function un.DisableUninstallCancellation
    IfSilent done
    GetDlgItem $0 $HWNDPARENT 2
    EnableWindow $0 0
    done:
FunctionEnd

Function .onInit
    ${IfNot} ${AtLeastWin10}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires Windows 10 or later." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    !insertmacro AcquireInstallerLifecycleMutex
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
    StrCpy $EnvironmentNotificationFailed "0"
    StrCpy $InstallMutationActive "0"
FunctionEnd

Function .onGUIEnd
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function .onInstSuccess
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function .onInstFailed
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function un.onInit
    ${IfNot} ${AtLeastWin10}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires Windows 10 or later." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    !insertmacro AcquireInstallerLifecycleMutex
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
    StrCpy $DeleteConfigurationOnUninstall "0"
    StrCpy $EnvironmentNotificationFailed "0"
    StrCpy $InstallMutationActive "0"
    StrCpy $PartialCleanup "0"
    ${GetParameters} $0
    StrCmp $0 "" done
    StrCmp $0 "/S" done
    StrCmp $0 "/DELETE_CONFIG" delete_config
    StrCmp $0 "/S /DELETE_CONFIG" delete_config
    StrCmp $0 "/DELETE_CONFIG /S" delete_config
    MessageBox MB_ICONSTOP|MB_OK "Unsupported uninstall arguments. Use only /S and the exact /DELETE_CONFIG option." /SD IDOK
    SetErrorLevel ${APP_EXIT_INVALID_ARGUMENTS}
    Quit
    delete_config:
        StrCpy $DeleteConfigurationOnUninstall "1"
    done:
FunctionEnd

Function un.onGUIEnd
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function un.onUninstSuccess
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function un.onUninstFailed
    !insertmacro ReleaseInstallerLifecycleMutex
FunctionEnd

Function un.DeleteConfigurationPage
    IfSilent done 0
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}

    ${NSD_CreateLabel} 0 0 100% 72u "Uninstall removes ${APP_DISPLAY_NAME} machine-local state, SSH integration, and the configured package/tool cache. A running Sandbox stays open but becomes unmanaged; close it manually when finished. Select this option to also remove ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}. Project ${APP_PROJECT_DIRECTORY} profiles are not removed."
    Pop $0
    ${NSD_CreateCheckbox} 0 82u 100% 14u "Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}"
    Pop $DeleteConfigurationCheckbox
    ${If} $DeleteConfigurationOnUninstall == "1"
        ${NSD_Check} $DeleteConfigurationCheckbox
    ${EndIf}
    nsDialogs::Show
    done:
FunctionEnd

Function un.DeleteConfigurationPageLeave
    IfSilent done 0
    ${If} $DeleteConfigurationCheckbox == ""
        Goto done
    ${EndIf}
    ${NSD_GetState} $DeleteConfigurationCheckbox $0
    ${If} $0 == ${BST_CHECKED}
        StrCpy $DeleteConfigurationOnUninstall "1"
    ${Else}
        StrCpy $DeleteConfigurationOnUninstall "0"
    ${EndIf}
    done:
FunctionEnd

!macro DefineCheckInstallDirectoryFunction PREFIX
Function ${PREFIX}CheckInstallDirectory
    StrCpy $InstallDirectorySafe "1"
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR") i.r0 ?e'
    Pop $2
    ${If} $0 == -1
        ${If} $2 != ${APP_ERROR_FILE_NOT_FOUND}
        ${AndIf} $2 != ${APP_ERROR_PATH_NOT_FOUND}
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
    ${Else}
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        ${If} $1 == 0
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $1 != 0
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
    ${EndIf}
FunctionEnd
!macroend

!macro DefineCheckOwnershipMarkerFunction PREFIX
Function ${PREFIX}CheckOwnershipMarker
    StrCpy $OwnershipMarkerValid "0"
    System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR\${APP_INSTALLER_MARKER}") i.r0'
    ${If} $0 == -1
        Return
    ${EndIf}
    IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
    ${If} $1 != 0
        Return
    ${EndIf}
    IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
    ${If} $1 != 0
        Return
    ${EndIf}
    ClearErrors
    FileOpen $0 "$INSTDIR\${APP_INSTALLER_MARKER}" r
    ${If} ${Errors}
        Return
    ${EndIf}
    marker_read_next:
        ClearErrors
        FileRead $0 $1
        ${If} ${Errors}
            FileClose $0
            Return
        ${EndIf}
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":1}$\r$\n' marker_found
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":1}$\n' marker_found
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":1}' marker_found
        ; v0.0.10 wrote this same GUID through Windows PowerShell 5.1 JSON.
        StrCmp $1 '    "productGuid":  "${APP_PRODUCT_GUID}",$\r$\n' marker_found
        StrCmp $1 '    "productGuid":  "${APP_PRODUCT_GUID}",$\n' marker_found
        Goto marker_read_next
    marker_found:
        FileClose $0
        StrCpy $OwnershipMarkerValid "1"
FunctionEnd
!macroend

!insertmacro DefineCheckInstallDirectoryFunction ""
!insertmacro DefineCheckInstallDirectoryFunction "un."
!insertmacro DefineCheckOwnershipMarkerFunction ""
!insertmacro DefineCheckOwnershipMarkerFunction "un."

Function un.CheckInstallDirectoryResidual
    ; Preserve the marker unless enumeration proves that it is the sole entry.
    StrCpy $InstallDirectoryHasUnknownEntries "error"
    ClearErrors
    FindFirst $0 $1 "$INSTDIR\*"
    ${If} ${Errors}
        Return
    ${EndIf}
    StrCpy $InstallDirectoryHasUnknownEntries "0"
    residual_next:
        StrCmp $1 "" residual_done
        StrCmp $1 "." residual_advance
        StrCmp $1 ".." residual_advance
        StrCmp $1 "${APP_INSTALLER_MARKER}" residual_advance
        StrCpy $InstallDirectoryHasUnknownEntries "1"
        FindClose $0
        Return
    residual_advance:
        ClearErrors
        FindNext $0 $1
        ${If} ${Errors}
            Goto residual_done
        ${EndIf}
        Goto residual_next
    residual_done:
        FindClose $0
FunctionEnd

!macro ReadExistingPathOwnership
    ClearErrors
    ReadRegDWORD $PathOwnedPreviously HKCU "${UNINSTALL_KEY}" "PathAdded"
    ${If} ${Errors}
        StrCpy $PathOwnedPreviously "0"
    ${ElseIf} $PathOwnedPreviously != "1"
        StrCpy $PathOwnedPreviously "0"
    ${EndIf}
    ClearErrors
    ReadRegDWORD $PathAddPending HKCU "${UNINSTALL_KEY}" "PathAddPending"
    ${If} ${Errors}
        StrCpy $PathAddPending "0"
    ${ElseIf} $PathAddPending != "1"
        StrCpy $PathAddPending "0"
    ${EndIf}
!macroend

Function un.RestoreRetryOwnership
    StrCpy $RollbackFailed "0"
    ClearErrors
    CreateDirectory "$INSTDIR"
    ${IfNot} ${FileExists} "$INSTDIR\uninstall.exe"
        ClearErrors
        CopyFiles /SILENT "$EXEPATH" "$INSTDIR\uninstall.exe"
        ${If} ${Errors}
            StrCpy $RollbackFailed "1"
        ${EndIf}
    ${EndIf}
    ClearErrors
    FileOpen $0 "$INSTDIR\${APP_INSTALLER_MARKER}" w
    ${IfNot} ${Errors}
        FileWrite $0 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":1}$\r$\n'
        FileClose $0
    ${EndIf}
    ${If} ${Errors}
        StrCpy $RollbackFailed "1"
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    ${If} ${FileExists} "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}"
        WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}" -Uninstaller "$INSTDIR\uninstall.exe" -InstallDirectory "$INSTDIR"'
    ${EndIf}
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" 0
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupIncomplete" $PartialCleanup
    ${If} ${Errors}
        StrCpy $RollbackFailed "1"
        Return
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
    ${If} ${Errors}
        StrCpy $RollbackFailed "1"
    ${EndIf}
FunctionEnd

!macro RunPathHelper ACTION
    InitPluginsDir
    SetOutPath "$PLUGINSDIR\control"
    ClearErrors
    File "/oname=path.ps1" "${PATH_HELPER}"
    ${If} ${Errors}
        StrCpy $0 "error"
        StrCpy $1 "The installer could not extract its PATH helper."
    ${Else}
        nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$PLUGINSDIR\control\path.ps1" -Action ${ACTION} -InstallDirectory "$INSTDIR"'
        Pop $0
        Pop $1
    ${EndIf}
!macroend

!macro NotifyPathChanged
    DetailPrint "Notifying Windows about the PATH change..."
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'USER32::SendMessageTimeoutW(p ${HWND_BROADCAST}, i ${WM_SETTINGCHANGE}, p 0, w "Environment", i 0x2, i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}, *p .r3) p.r2 ?e'
    Pop $4
    ${If} $2 == 0
        StrCpy $EnvironmentNotificationFailed "1"
        DetailPrint "Windows did not confirm the PATH-change notification (error $4)."
    ${EndIf}
!macroend

!macro BackupOwnedFile NAME FLAG
    StrCpy ${FLAG} "0"
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR\${NAME}") i.r0'
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $0 == -1
        ${OrIf} $1 != 0
        ${OrIf} $2 != 0
            StrCpy $BackupFailed "1"
        ${Else}
            ClearErrors
            CopyFiles /SILENT "$INSTDIR\${NAME}" "$PLUGINSDIR\backup"
            ${If} ${Errors}
                StrCpy $BackupFailed "1"
            ${Else}
                StrCpy ${FLAG} "1"
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

!macro ReplaceOwnedFile NAME
    ${If} $PayloadCopyFailed == "0"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\package\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not install ${NAME}."
        ${EndIf}
    ${EndIf}
!macroend

!macro RestoreOwnedFile NAME FLAG
    ${If} ${FLAG} == "1"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\backup\${NAME}" "$INSTDIR"
    ${Else}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
    ${EndIf}
    ${If} ${Errors}
        StrCpy $RollbackFailed "1"
    ${EndIf}
!macroend

!macro PreflightOwnedFile NAME
    ${IfNot} ${FileExists} "$INSTDIR\${NAME}"
        ${If} $CleanupComplete != "1"
            DetailPrint "Required installer-owned file is missing: ${NAME}"
            StrCpy $UninstallPreflightFailed "1"
        ${EndIf}
    ${Else}
        System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR\${NAME}") i.r0'
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $0 == -1
        ${OrIf} $1 != 0
        ${OrIf} $2 != 0
            DetailPrint "Installer-owned path is not a regular file: ${NAME}"
            StrCpy $UninstallPreflightFailed "1"
        ${Else}
            System::Call 'KERNEL32::CreateFileW(w "$INSTDIR\${NAME}", i ${APP_DELETE_ACCESS}, i ${APP_FILE_SHARE_ALL}, p 0, i ${APP_OPEN_EXISTING}, i 0, p 0) p.r0 ?e'
            Pop $1
            ${If} $0 == -1
                DetailPrint "Installer-owned file is not ready for deletion: ${NAME} (error $1)"
                StrCpy $UninstallPreflightFailed "1"
            ${Else}
                System::Call 'KERNEL32::CloseHandle(p r0)'
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

!macro DeleteOwnedFile NAME
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove ${NAME}."
            Goto uninstall_finalize_failure
        ${EndIf}
    ${EndIf}
!macroend

!macro DeleteOwnedFileAfterRegistration NAME
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove ${NAME}."
            Call un.RestoreRetryOwnership
            Goto uninstall_finalize_failure
        ${EndIf}
    ${EndIf}
!macroend

Section "Install"
    DetailPrint "Validating and installing ${APP_DISPLAY_NAME} ${VERSION}..."
    StrCpy $ExistingRegistrationOwned "0"
    StrCpy $PathOwnedPreviously "0"
    StrCpy $PathPresentBefore "0"
    StrCpy $PathAddedThisRun "0"
    StrCpy $PathAddPending "0"
    StrCpy $InstallCompleteWasComplete "0"
    StrCpy $BackupFailed "0"
    StrCpy $PayloadCopyFailed "0"
    StrCpy $RollbackFailed "0"
    StrCpy $InstallFailureMessage ""

    Call CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The fixed ${APP_DISPLAY_NAME} install path is not a regular directory. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}
    ${DirState} "$INSTDIR" $2
    Call CheckOwnershipMarker

    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${IfNot} ${Errors}
        ${If} $0 != "${APP_PRODUCT_GUID}"
            MessageBox MB_ICONSTOP|MB_OK "The product-GUID registration at the fixed ${APP_DISPLAY_NAME} key does not match this installer. No files were changed." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ClearErrors
        ReadRegStr $1 HKCU "${UNINSTALL_KEY}" "InstallLocation"
        ${If} ${Errors}
        ${OrIf} $1 != "$INSTDIR"
            MessageBox MB_ICONSTOP|MB_OK "The registered ${APP_DISPLAY_NAME} install location does not match the fixed installer location. No files were changed." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ${If} $OwnershipMarkerValid != "1"
            MessageBox MB_ICONSTOP|MB_OK "The registered ${APP_DISPLAY_NAME} directory has no matching ownership marker. No files were changed." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        StrCpy $ExistingRegistrationOwned "1"
        ClearErrors
        ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
        ${IfNot} ${Errors}
        ${AndIf} $0 == "1"
            StrCpy $InstallCompleteWasComplete "1"
        ${EndIf}
        !insertmacro ReadExistingPathOwnership
    ${Else}
        ClearErrors
        EnumRegValue $0 HKCU "${UNINSTALL_KEY}" 0
        ${IfNot} ${Errors}
            ${If} $OwnershipMarkerValid != "1"
                MessageBox MB_ICONSTOP|MB_OK "The fixed product-GUID registry key contains state without a matching ProductGuid or ownership marker. No files were changed." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
            ClearErrors
            ReadRegStr $1 HKCU "${UNINSTALL_KEY}" "InstallLocation"
            ${IfNot} ${Errors}
            ${AndIf} $1 != "$INSTDIR"
                MessageBox MB_ICONSTOP|MB_OK "The incomplete ${APP_DISPLAY_NAME} registration points to another location. No files were changed." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
            ; A valid marker makes this the exact incomplete registration shape
            ; left if setup stops before ProductGuid commits. Rewrite it in place.
            StrCpy $ExistingRegistrationOwned "1"
            !insertmacro ReadExistingPathOwnership
        ${Else}
            ${If} $2 == "1"
            ${AndIf} $OwnershipMarkerValid != "1"
                MessageBox MB_ICONSTOP|MB_OK "The fixed ${APP_DISPLAY_NAME} install directory is nonempty but unmarked. Its contents were preserved." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
        ${EndIf}
    ${EndIf}

    InitPluginsDir
    SetOutPath "$PLUGINSDIR\package"
    ClearErrors
    File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"
    File "${PACKAGE_DIR}\${APP_LICENSE}"
    File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"
    File "/oname=${APP_QUIET_UNINSTALL_HELPER}" "${QUIET_UNINSTALL_HELPER}"
    File "${PACKAGE_DIR}\${APP_EXECUTABLE}"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the complete ${APP_DISPLAY_NAME} application package. No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}
    ClearErrors
    WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the ${APP_DISPLAY_NAME} uninstaller. No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}
    ClearErrors
    FileOpen $0 "$PLUGINSDIR\package\${APP_INSTALLER_MARKER}" w
    ${IfNot} ${Errors}
        FileWrite $0 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":1}$\r$\n'
        FileClose $0
    ${EndIf}
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the ${APP_DISPLAY_NAME} ownership marker. No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}

    CreateDirectory "$PLUGINSDIR\backup"
    ClearErrors
    CreateDirectory "$INSTDIR"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the fixed ${APP_DISPLAY_NAME} install directory. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}
    !insertmacro BackupOwnedFile "${APP_BASE_SCRIPT}" $BackupBaseScript
    !insertmacro BackupOwnedFile "${APP_LICENSE}" $BackupLicense
    !insertmacro BackupOwnedFile "${APP_STACK_SCRIPT}" $BackupStackScript
    !insertmacro BackupOwnedFile "${APP_QUIET_UNINSTALL_HELPER}" $BackupQuietUninstall
    !insertmacro BackupOwnedFile "uninstall.exe" $BackupUninstaller
    !insertmacro BackupOwnedFile "${APP_EXECUTABLE}" $BackupExecutable
    ${If} $ExistingRegistrationOwned == "1"
        !insertmacro BackupOwnedFile "${APP_REPLACED_EXECUTABLE}" $BackupReplacedExecutable
    ${Else}
        StrCpy $BackupReplacedExecutable "0"
    ${EndIf}
    !insertmacro BackupOwnedFile "${APP_INSTALLER_MARKER}" $BackupMarker
    ${If} $BackupFailed != "0"
        MessageBox MB_ICONSTOP|MB_OK "Could not back up every existing installer-owned file. Close running commands and try again. No installed files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}

    StrCpy $InstallMutationActive "1"
    Call DisableInstallCancellation
    ${If} $ExistingRegistrationOwned == "1"
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
        ${If} ${Errors}
            StrCpy $InstallMutationActive "0"
            Call EnableInstallCancellation
            MessageBox MB_ICONSTOP|MB_OK "Could not mark the existing ${APP_DISPLAY_NAME} installation incomplete before replacement. No installed files were changed." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
    ${EndIf}

    ; The marker is first so a hard interruption leaves an identifiable root.
    ; Support files are replaced before the executable used by application cleanup.
    !insertmacro ReplaceOwnedFile "${APP_INSTALLER_MARKER}"
    !insertmacro ReplaceOwnedFile "${APP_BASE_SCRIPT}"
    !insertmacro ReplaceOwnedFile "${APP_LICENSE}"
    !insertmacro ReplaceOwnedFile "${APP_STACK_SCRIPT}"
    !insertmacro ReplaceOwnedFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro ReplaceOwnedFile "uninstall.exe"
    !insertmacro ReplaceOwnedFile "${APP_EXECUTABLE}"
    ${If} $BackupReplacedExecutable == "1"
        ClearErrors
        Delete "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not remove replaced executable ${APP_REPLACED_EXECUTABLE}."
        ${EndIf}
    ${EndIf}
    ${If} $PayloadCopyFailed != "0"
        Goto install_payload_rollback
    ${EndIf}

    !insertmacro RunPathHelper "Contains"
    ${If} $0 != "0"
        StrCpy $InstallFailureMessage "The PATH helper could not inspect the current-user PATH: status $0. $1"
        Goto install_integration_failure
    ${EndIf}
    ${If} $1 == "1"
        StrCpy $PathPresentBefore "1"
    ${ElseIf} $1 != "0"
        StrCpy $InstallFailureMessage "The PATH helper returned an invalid Contains result."
        Goto install_integration_failure
    ${EndIf}
    ${If} $PathAddPending == "1"
    ${AndIf} $PathPresentBefore == "1"
        StrCpy $PathOwnedPreviously "1"
    ${EndIf}
    ${If} $PathPresentBefore == "0"
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAddPending" 1
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "PATH ownership intent could not be recorded before adding the install directory."
            Goto install_integration_failure
        ${EndIf}
        StrCpy $PathAddPending "1"
        !insertmacro RunPathHelper "Add"
        ${If} $0 == "10"
            StrCpy $PathAddedThisRun "1"
        ${ElseIf} $0 == "0"
            !insertmacro RunPathHelper "Contains"
            ${If} $0 != "0"
            ${OrIf} $1 != "1"
                StrCpy $InstallFailureMessage "The current-user PATH did not contain the install directory after Add."
                Goto install_integration_failure
            ${EndIf}
            StrCpy $PathAddedThisRun "1"
        ${Else}
            StrCpy $InstallFailureMessage "The PATH helper could not add the install directory: status $0. $1"
            Goto install_integration_failure
        ${EndIf}
    ${EndIf}

    StrCpy $3 "0"
    ${If} $PathOwnedPreviously == "1"
        StrCpy $3 "1"
    ${ElseIf} $PathAddedThisRun == "1"
        StrCpy $3 "1"
    ${EndIf}

    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\${APP_EXECUTABLE}",0'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}" -Uninstaller "$INSTDIR\uninstall.exe" -InstallDirectory "$INSTDIR"'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $3
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Windows Installed Apps registration could not be written."
        Goto install_integration_failure
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "ProductGuid could not be recorded."
        Goto install_integration_failure
    ${EndIf}
    ${If} $PathAddPending == "1"
        ClearErrors
        DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "PATH ownership intent could not be cleared after PathAdded was committed."
            Goto install_integration_failure
        ${EndIf}
        StrCpy $PathAddPending "0"
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupIncomplete" 0
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "CleanupIncomplete could not be reset."
        Goto install_integration_failure
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "InstallComplete could not be recorded."
        Goto install_integration_failure
    ${EndIf}

    ; Remove superseded transaction-engine values after the direct format commits.
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "InstallationId"
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathEntry"
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "UninstallPhase"
    ClearErrors

    StrCpy $InstallMutationActive "0"
    Call EnableInstallCancellation
    ${If} $3 == "1"
        !insertmacro NotifyPathChanged
    ${EndIf}

    ; Configuration is deliberately outside payload and registration commit.
    DetailPrint "Creating the default user configuration when missing..."
    SetOutPath "$INSTDIR"
    nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-seed-configuration'
    Pop $0
    Pop $1
    ${If} $0 == "error"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but Windows could not start default-configuration creation. The first command that needs configuration will try again." /SD IDOK
    ${ElseIf} $0 != "0"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but default configuration could not be created yet: application status $0. $1 The first command that needs configuration will try again." /SD IDOK
    ${EndIf}
    ${If} $EnvironmentNotificationFailed == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but Windows did not confirm the PATH refresh. Open a new terminal. If it still cannot find ${APP_NAME}, sign out and back in." /SD IDOK
    ${EndIf}
    Goto install_done

    install_integration_failure:
        StrCpy $RollbackFailed "0"
        ${If} $PathAddedThisRun == "1"
            !insertmacro RunPathHelper "Remove"
            ${If} $0 != "0"
            ${AndIf} $0 != "10"
                StrCpy $RollbackFailed "1"
            ${ElseIf} $PathAddPending == "1"
                ClearErrors
                DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
                ${If} ${Errors}
                    StrCpy $RollbackFailed "1"
                ${Else}
                    StrCpy $PathAddPending "0"
                ${EndIf}
            ${EndIf}
        ${ElseIf} $PathAddPending == "1"
            StrCpy $RollbackFailed "1"
        ${EndIf}
        ${If} $ExistingRegistrationOwned == "0"
        ${AndIf} $RollbackFailed == "0"
            ClearErrors
            DeleteRegKey HKCU "${UNINSTALL_KEY}"
            ${If} ${Errors}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}
        StrCpy $InstallMutationActive "0"
        Call EnableInstallCancellation
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The working application payload remains installed. Run setup again to repair Windows registration." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The working application payload remains installed, and partial PATH or registry integration may also remain. Run setup again to repair it." /SD IDOK
        ${EndIf}
        SetErrorLevel ${APP_EXIT_INSTALL_INTEGRATION}
        Quit

    install_payload_rollback:
        StrCpy $RollbackFailed "0"
        !insertmacro RestoreOwnedFile "${APP_EXECUTABLE}" $BackupExecutable
        ${If} $ExistingRegistrationOwned == "1"
            !insertmacro RestoreOwnedFile "${APP_REPLACED_EXECUTABLE}" $BackupReplacedExecutable
        ${EndIf}
        !insertmacro RestoreOwnedFile "uninstall.exe" $BackupUninstaller
        !insertmacro RestoreOwnedFile "${APP_QUIET_UNINSTALL_HELPER}" $BackupQuietUninstall
        !insertmacro RestoreOwnedFile "${APP_STACK_SCRIPT}" $BackupStackScript
        !insertmacro RestoreOwnedFile "${APP_LICENSE}" $BackupLicense
        !insertmacro RestoreOwnedFile "${APP_BASE_SCRIPT}" $BackupBaseScript
        !insertmacro RestoreOwnedFile "${APP_INSTALLER_MARKER}" $BackupMarker
        ClearErrors
        RMDir "$INSTDIR"
        ${If} $RollbackFailed == "0"
        ${AndIf} $ExistingRegistrationOwned == "1"
        ${AndIf} $InstallCompleteWasComplete == "1"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
            ${If} ${Errors}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}
        StrCpy $InstallMutationActive "0"
        Call EnableInstallCancellation
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The prior installer-owned files were restored. Close running commands and run setup again." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Rollback was incomplete. Close running commands and run setup again before using the installed command." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_ROLLBACK_INCOMPLETE}
        ${EndIf}
        Quit

    install_done:
SectionEnd

Section "Uninstall"
    SetAutoClose true
    SetOutPath "$TEMP"
    StrCpy $CleanupComplete "0"
    StrCpy $UninstallPreflightFailed "0"
    StrCpy $InstallFailureMessage ""
    StrCpy $RollbackFailed "0"

    DetailPrint "Validating ${APP_DISPLAY_NAME} ownership and deletion readiness..."
    Call un.CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The fixed ${APP_DISPLAY_NAME} install path is not a regular directory. No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${If} ${Errors}
    ${OrIf} $0 != "${APP_PRODUCT_GUID}"
        MessageBox MB_ICONSTOP|MB_OK "The product-GUID registration does not match this ${APP_DISPLAY_NAME} uninstaller. No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
    ${If} ${Errors}
    ${OrIf} $0 != "$INSTDIR"
        MessageBox MB_ICONSTOP|MB_OK "The registered install location does not match this ${APP_DISPLAY_NAME} uninstaller. No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
    ${If} ${Errors}
    ${OrIf} $0 != "1"
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} registration is not marked complete. Run setup to repair it before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}
    Call un.CheckOwnershipMarker
    ${If} $OwnershipMarkerValid != "1"
        MessageBox MB_ICONSTOP|MB_OK "The ${APP_DISPLAY_NAME} ownership marker is missing or does not match. No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $CleanupComplete HKCU "${UNINSTALL_KEY}" "CleanupComplete"
    ${If} ${Errors}
        StrCpy $CleanupComplete "0"
    ${ElseIf} $CleanupComplete != "1"
        StrCpy $CleanupComplete "0"
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "CleanupIncomplete"
    ${IfNot} ${Errors}
    ${AndIf} $0 == "1"
        StrCpy $PartialCleanup "1"
    ${EndIf}
    ClearErrors
    ReadRegDWORD $PathOwnedPreviously HKCU "${UNINSTALL_KEY}" "PathAdded"
    ${If} ${Errors}
        StrCpy $PathOwnedPreviously "0"
    ${ElseIf} $PathOwnedPreviously != "1"
        StrCpy $PathOwnedPreviously "0"
    ${EndIf}

    !insertmacro PreflightOwnedFile "${APP_BASE_SCRIPT}"
    !insertmacro PreflightOwnedFile "${APP_LICENSE}"
    !insertmacro PreflightOwnedFile "${APP_STACK_SCRIPT}"
    !insertmacro PreflightOwnedFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro PreflightOwnedFile "${APP_EXECUTABLE}"
    !insertmacro PreflightOwnedFile "${APP_INSTALLER_MARKER}"
    !insertmacro PreflightOwnedFile "uninstall.exe"
    ${If} $UninstallPreflightFailed != "0"
        MessageBox MB_ICONSTOP|MB_OK "One or more installer-owned files are missing, replaced, or in use. No application cleanup or file removal was started. Close running commands and try again." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}

    StrCpy $InstallMutationActive "1"
    Call un.DisableUninstallCancellation

    ${If} $CleanupComplete != "1"
        DetailPrint "Removing ${APP_DISPLAY_NAME} state, SSH integration, and cache; any running Sandbox stays open..."
        ${If} $DeleteConfigurationOnUninstall == "1"
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1 --delete-configuration'
        ${Else}
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1'
        ${EndIf}
        Pop $0
        Pop $1
        ${If} $0 != "0"
            ${If} $0 == "error"
                StrCpy $InstallFailureMessage "Windows could not start ${APP_DISPLAY_NAME} application cleanup."
            ${Else}
                StrCpy $InstallFailureMessage "${APP_DISPLAY_NAME} application cleanup returned status $0. $1"
            ${EndIf}
            IfSilent uninstall_cleanup_abort
            MessageBox MB_ICONEXCLAMATION|MB_YESNO "$InstallFailureMessage Continue removing the application files while preserving residual machine-local state?" /SD IDNO IDYES uninstall_cleanup_continue IDNO uninstall_cleanup_abort
            uninstall_cleanup_continue:
                StrCpy $PartialCleanup "1"
                ClearErrors
                WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupIncomplete" 1
                ${If} ${Errors}
                    MessageBox MB_ICONSTOP|MB_OK "The incomplete application-cleanup outcome could not be recorded. No application files or installer registration were removed. Run uninstall again." /SD IDOK
                    SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
                    Quit
                ${EndIf}
                Goto uninstall_record_cleanup
            uninstall_cleanup_abort:
                MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage No application files or installer registration were removed." /SD IDOK
                SetErrorLevel ${APP_EXIT_UNINSTALL_CLEANUP}
                Quit
        ${EndIf}
        StrCpy $PartialCleanup "0"
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupIncomplete" 0
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "The successful application-cleanup outcome could not be recorded. No application files or installer registration were removed. Run uninstall again." /SD IDOK
            SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
            Quit
        ${EndIf}
        uninstall_record_cleanup:
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "The application-cleanup decision could not be recorded. No application files or installer registration were removed. Run uninstall again." /SD IDOK
            SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
            Quit
        ${EndIf}
        StrCpy $CleanupComplete "1"
    ${Else}
        DetailPrint "Application cleanup was already completed; continuing installer-owned removal."
    ${EndIf}

    ${If} $PathOwnedPreviously == "1"
        !insertmacro RunPathHelper "Remove"
        ${If} $0 != "0"
        ${AndIf} $0 != "10"
            MessageBox MB_ICONSTOP|MB_OK "Could not remove the installer-owned PATH entry: status $0. $1 Application cleanup is recorded complete, but no application files or registration were removed. Run uninstall again." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FINALIZE}
            Quit
        ${EndIf}
        ${If} $0 == "10"
            !insertmacro NotifyPathChanged
        ${EndIf}
    ${EndIf}

    !insertmacro DeleteOwnedFile "${APP_BASE_SCRIPT}"
    !insertmacro DeleteOwnedFile "${APP_LICENSE}"
    !insertmacro DeleteOwnedFile "${APP_STACK_SCRIPT}"
    !insertmacro DeleteOwnedFile "${APP_EXECUTABLE}"

    ClearErrors
    DeleteRegKey HKCU "${UNINSTALL_KEY}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Could not remove the product-GUID registration."
        Call un.RestoreRetryOwnership
        Goto uninstall_finalize_failure
    ${EndIf}

    !insertmacro DeleteOwnedFileAfterRegistration "uninstall.exe"
    !insertmacro DeleteOwnedFileAfterRegistration "${APP_QUIET_UNINSTALL_HELPER}"

    Call un.CheckInstallDirectoryResidual
    ${If} $InstallDirectoryHasUnknownEntries == "error"
        StrCpy $InstallFailureMessage "Could not inspect the remaining install directory."
        Call un.RestoreRetryOwnership
        Goto uninstall_finalize_failure
    ${ElseIf} $InstallDirectoryHasUnknownEntries == "1"
        DetailPrint "The install directory contains non-installer files; its ownership marker and directory were preserved."
    ${Else}
        !insertmacro DeleteOwnedFileAfterRegistration "${APP_INSTALLER_MARKER}"
        ClearErrors
        RMDir "$INSTDIR"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove the install directory."
            Call un.RestoreRetryOwnership
            Goto uninstall_finalize_failure
        ${EndIf}
    ${EndIf}
    ${If} $EnvironmentNotificationFailed == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} was removed, but Windows did not confirm the PATH refresh. Open a new terminal. If it still sees the old PATH, sign out and back in." /SD IDOK
    ${EndIf}
    ${If} $PartialCleanup == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} application files were removed, but application cleanup did not complete. Residual machine-local state was preserved." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PARTIAL_CLEANUP}
    ${EndIf}
    StrCpy $InstallMutationActive "0"
    Goto uninstall_done

    uninstall_finalize_failure:
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage CleanupComplete and retry ownership remain recorded, so retrying uninstall will not repeat application cleanup." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Retry ownership could not be fully restored. Preserve the remaining files and registration for diagnosis." /SD IDOK
        ${EndIf}
        SetErrorLevel ${APP_EXIT_UNINSTALL_FINALIZE}
        Quit

    uninstall_done:
SectionEnd
