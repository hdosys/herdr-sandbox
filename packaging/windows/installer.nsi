Unicode true

!macro Require NAME
!ifndef ${NAME}
    !error "${NAME} is required"
!endif
!macroend

!macro RequireValue NAME VALUE
!if "${VALUE}" == ""
    !error "${NAME} must not be empty"
!endif
!macroend

!macro RejectLeafCharacter NAME VALUE CHARACTER
    !undef /noerrors _VALIDATE_BAD
    !searchparse /noerrors "${VALUE}" "${CHARACTER}" _VALIDATE_BAD
    !ifdef _VALIDATE_BAD
        !error "${NAME} must be a safe leaf name without path separators or reserved characters"
    !endif
    !undef /noerrors _VALIDATE_BAD
!macroend

!macro ValidateLeaf NAME VALUE
    !insertmacro RequireValue ${NAME} "${VALUE}"
    !if "${VALUE}" == "."
        !error "${NAME} must not be ."
    !endif
    !if "${VALUE}" == ".."
        !error "${NAME} must not be .."
    !endif
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "\"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "/"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" ":"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "*"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "?"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "<"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" ">"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "|"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" ";"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "$$"
    !insertmacro RejectLeafCharacter ${NAME} "${VALUE}" "$\""
!macroend

!macro AssertDifferent A_NAME A_VALUE B_NAME B_VALUE
!if "${A_VALUE}" == "${B_VALUE}"
    !error "${A_NAME} collides with ${B_NAME}"
!endif
!macroend

!insertmacro Require VERSION
!insertmacro Require FIXED_VERSION
!insertmacro Require PACKAGE_DIR
!insertmacro Require PATH_HELPER
!insertmacro Require QUIET_UNINSTALL_HELPER
!insertmacro Require OUTPUT_FILE
!insertmacro Require OUTPUT_FILE_NAME
!insertmacro Require APP_NAME
!insertmacro Require APP_APPLICATION_NAME
!insertmacro Require APP_DISPLAY_NAME
!insertmacro Require APP_EXECUTABLE
!insertmacro Require APP_BASE_SCRIPT
!insertmacro Require APP_STACK_SCRIPT
!insertmacro Require APP_LICENSE
!insertmacro Require APP_CONFIG_FILE
!insertmacro Require APP_USER_SCRIPT
!insertmacro Require APP_PROJECT_DIRECTORY
!insertmacro Require APP_INSTALL_DIRECTORY
!insertmacro Require APP_PUBLISHER
!insertmacro Require APP_PRODUCT_URL
!insertmacro Require APP_PRODUCT_GUID
!insertmacro Require APP_UNINSTALL_KEY
!insertmacro Require APP_QUIET_UNINSTALL_HELPER
!insertmacro Require APP_COPYRIGHT

!insertmacro RequireValue VERSION "${VERSION}"
!insertmacro RequireValue FIXED_VERSION "${FIXED_VERSION}"
!insertmacro RequireValue PACKAGE_DIR "${PACKAGE_DIR}"
!insertmacro RequireValue PATH_HELPER "${PATH_HELPER}"
!insertmacro RequireValue QUIET_UNINSTALL_HELPER "${QUIET_UNINSTALL_HELPER}"
!insertmacro RequireValue OUTPUT_FILE "${OUTPUT_FILE}"
!insertmacro RequireValue APP_NAME "${APP_NAME}"
!insertmacro RequireValue APP_APPLICATION_NAME "${APP_APPLICATION_NAME}"
!insertmacro RequireValue APP_DISPLAY_NAME "${APP_DISPLAY_NAME}"
!insertmacro RequireValue APP_PUBLISHER "${APP_PUBLISHER}"
!insertmacro RequireValue APP_PRODUCT_URL "${APP_PRODUCT_URL}"
!insertmacro RequireValue APP_PRODUCT_GUID "${APP_PRODUCT_GUID}"
!insertmacro RequireValue APP_COPYRIGHT "${APP_COPYRIGHT}"

!insertmacro ValidateLeaf OUTPUT_FILE_NAME "${OUTPUT_FILE_NAME}"
!insertmacro ValidateLeaf APP_APPLICATION_NAME "${APP_APPLICATION_NAME}"
!insertmacro ValidateLeaf APP_EXECUTABLE "${APP_EXECUTABLE}"
!insertmacro ValidateLeaf APP_BASE_SCRIPT "${APP_BASE_SCRIPT}"
!insertmacro ValidateLeaf APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
!insertmacro ValidateLeaf APP_LICENSE "${APP_LICENSE}"
!insertmacro ValidateLeaf APP_INSTALL_DIRECTORY "${APP_INSTALL_DIRECTORY}"
!insertmacro ValidateLeaf APP_PRODUCT_GUID "${APP_PRODUCT_GUID}"
!insertmacro ValidateLeaf APP_UNINSTALL_KEY "${APP_UNINSTALL_KEY}"
!insertmacro ValidateLeaf APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"

!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_BASE_SCRIPT "${APP_BASE_SCRIPT}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_LICENSE "${APP_LICENSE}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_LICENSE "${APP_LICENSE}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}" uninstall.exe "uninstall.exe"

!ifndef ASSETS_DIR
    !define ASSETS_DIR "${__FILEDIR__}\assets"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "WinVer.nsh"
!include "x64.nsh"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_KEY}"
!ifndef APP_INSTALLER_MUTEX_NAME
    ; Stable installer-only gate. It must not be versioned between releases.
    !define APP_INSTALLER_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerExclusive"
!endif
!ifndef APP_LIFECYCLE_MUTEX_NAME
    ; Must match the stable name used by application lifecycle commands.
    !define APP_LIFECYCLE_MUTEX_NAME "Local\${APP_APPLICATION_NAME}-lifecycle-v1"
!endif
!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 250
!define APP_ERROR_FILE_NOT_FOUND 2
!define APP_ERROR_PATH_NOT_FOUND 3
!define APP_ERROR_ACCESS_DENIED 5
!define APP_ERROR_SHARING_VIOLATION 32
!define APP_ERROR_LOCK_VIOLATION 33
!define APP_ERROR_ALREADY_EXISTS 183
!define APP_WAIT_OBJECT_0 0
!define APP_WAIT_ABANDONED 128
!define APP_WAIT_TIMEOUT 258
!define APP_FILE_ATTRIBUTE_READONLY 0x1
!define APP_FILE_ATTRIBUTE_DIRECTORY 0x10
!define APP_FILE_ATTRIBUTE_REPARSE_POINT 0x400
!define APP_FILE_STATE_ABSENT 0
!define APP_FILE_STATE_REGULAR 1
!define APP_FILE_STATE_UNSAFE 2
!define APP_REGISTRY_KEY_STATE_ABSENT 0
!define APP_REGISTRY_KEY_STATE_PRESENT 1
!define APP_REGISTRY_KEY_STATE_UNSAFE 2
!define APP_REGISTRY_VALUE_STATE_ABSENT 0
!define APP_REGISTRY_VALUE_STATE_PRESENT 1
!define APP_REGISTRY_VALUE_STATE_UNSAFE 2
!define APP_REGISTRY_TYPE_SZ 1
!define APP_REGISTRY_TYPE_EXPAND_SZ 2
!define APP_REGISTRY_TYPE_DWORD 4
!define APP_ERROR_SUCCESS 0
!define APP_HKEY_CURRENT_USER 0x80000001
!define APP_KEY_READ_64 0x20119
!define APP_EXIT_LIFECYCLE_BUSY 41
!define APP_EXIT_UNSUPPORTED_PLATFORM 50
!define APP_EXIT_INSTALL_FAILED 70
!define APP_EXIT_UNINSTALL_FAILED 80

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var DeleteConfigurationUnavailable
Var InstallMutationActive
Var InstallerMutexHandle
Var LifecycleMutexHandle
Var ExistingInstallation
Var ExistingRegistryOwned
Var InstallDirectorySafe
Var InstallDirectoryExists
Var InstallDirectoryExistedBefore
Var InstallDirectoryCreatedThisRun
Var InstallDirectoryNonEmpty
Var FileState
Var OperationResult
Var RootRemovalError
Var RegistryRestoreFailed
Var RegistryValueState
Var RegistryValueType
Var PathAction
Var PathOwned
Var PathPending
Var PathNotificationRequired
Var BackupFailed
Var PayloadCopyFailed
Var RollbackFailed
Var InstallFailureMessage
Var CleanupComplete
Var CleanupRetryRequired
Var UninstallKeyState
Var RegistryExistedBefore
Var InstallIntentTokenWasPresent
Var InstallIntentTokenOriginal
Var InstallIntentTokenOriginalType
Var InstallIntentTokenValid
Var InstallCompleteWasPresent
Var InstallCompleteOriginal
Var InstallCompleteOriginalType
Var ProductGuidWasPresent
Var ProductGuidOriginal
Var ProductGuidOriginalType
Var InstallLocationWasPresent
Var InstallLocationOriginal
Var InstallLocationOriginalType

!searchreplace APP_DISPLAY_NAME_ESCAPED "${APP_DISPLAY_NAME}" "&" "&&"
Name "${APP_DISPLAY_NAME}" "${APP_DISPLAY_NAME_ESCAPED}"
!undef APP_DISPLAY_NAME_ESCAPED
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
ManifestSupportedOS all
ShowInstDetails show
ShowUninstDetails show
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

; Release builds can supply commands that sign the generated uninstaller before
; embedding and the final setup executable after compilation. Both commands must
; contain NSIS' %1 output-file placeholder and return zero.
!ifdef APP_UNINSTALL_FINALIZE_COMMAND
    !uninstfinalize '${APP_UNINSTALL_FINALIZE_COMMAND}' = 0
!endif
!ifdef APP_INSTALL_FINALIZE_COMMAND
    !finalize '${APP_INSTALL_FINALIZE_COMMAND}' = 0
!endif

!define MUI_ABORTWARNING
!define MUI_CUSTOMFUNCTION_ABORT PreventInstallMutationAbort
!define MUI_CUSTOMFUNCTION_UNABORT un.PreventUninstallMutationAbort
!define INSTALLER_WELCOME_BITMAP_100 "${ASSETS_DIR}\installer-welcome-finish-164x314.bmp"
!define INSTALLER_WELCOME_BITMAP_125 "${ASSETS_DIR}\installer-welcome-finish-205x393.bmp"
!define INSTALLER_WELCOME_BITMAP_150 "${ASSETS_DIR}\installer-welcome-finish-246x471.bmp"
!define INSTALLER_WELCOME_BITMAP_175 "${ASSETS_DIR}\installer-welcome-finish-287x550.bmp"
!define INSTALLER_WELCOME_BITMAP_200 "${ASSETS_DIR}\installer-welcome-finish-328x628.bmp"
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
!define MUI_FINISHPAGE_TEXT "Setup is complete. No app window opens. Open a new terminal:$\r$\n$\r$\n${APP_NAME} init: Create a project profile$\r$\n${APP_NAME} up: Start or reconnect$\r$\n${APP_NAME} config: Open the configuration file$\r$\n${APP_NAME} status: Inspect Sandbox state"
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
    InitPluginsDir
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
    ${If} $ExistingInstallation == "1"
        ${NSD_Uncheck} $mui.FinishPage.Run
        ShowWindow $mui.FinishPage.Run ${SW_HIDE}
        ${NSD_SetFocus} $mui.Button.Next
    ${EndIf}
FunctionEnd

!macro ReleaseInstallerMutex
    ${If} $InstallerMutexHandle != 0
        System::Call 'KERNEL32::ReleaseMutex(p $InstallerMutexHandle) i.r0'
        System::Call 'KERNEL32::CloseHandle(p $InstallerMutexHandle) i.r0'
        StrCpy $InstallerMutexHandle 0
    ${EndIf}
!macroend

!macro ReleaseLifecycleMutex
    ${If} $LifecycleMutexHandle != 0
        System::Call 'KERNEL32::ReleaseMutex(p $LifecycleMutexHandle) i.r0'
        System::Call 'KERNEL32::CloseHandle(p $LifecycleMutexHandle) i.r0'
        StrCpy $LifecycleMutexHandle 0
    ${EndIf}
!macroend

!macro ReleaseAllMutexes
    !insertmacro ReleaseLifecycleMutex
    !insertmacro ReleaseInstallerMutex
!macroend

!macro AcquireInstallerMutex FAILURE_CODE
    System::Call 'KERNEL32::CreateMutexW(p 0, i 0, w "${APP_INSTALLER_MUTEX_NAME}") p.r1'
    StrCpy $InstallerMutexHandle $1
    ${If} $1 == 0
        MessageBox MB_ICONSTOP|MB_OK "Windows could not create the ${APP_DISPLAY_NAME} installer gate." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
    System::Call 'KERNEL32::WaitForSingleObject(p $1, i 0) i.r0'
    ${If} $0 == ${APP_WAIT_OBJECT_0}
    ${OrIf} $0 == ${APP_WAIT_ABANDONED}
        Nop
    ${ElseIf} $0 == ${APP_WAIT_TIMEOUT}
        System::Call 'KERNEL32::CloseHandle(p $1) i.r0'
        StrCpy $InstallerMutexHandle 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "Another ${APP_DISPLAY_NAME} setup or uninstall is running." /SD IDOK
        SetErrorLevel ${APP_EXIT_LIFECYCLE_BUSY}
        Quit
    ${Else}
        System::Call 'KERNEL32::CloseHandle(p $1) i.r0'
        StrCpy $InstallerMutexHandle 0
        MessageBox MB_ICONSTOP|MB_OK "Windows could not acquire the ${APP_DISPLAY_NAME} installer gate." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
!macroend

!macro AcquireLifecycleMutex FAILURE_CODE
    System::Call 'KERNEL32::CreateMutexW(p 0, i 0, w "${APP_LIFECYCLE_MUTEX_NAME}") p.r1'
    StrCpy $LifecycleMutexHandle $1
    ${If} $1 == 0
        !insertmacro ReleaseInstallerMutex
        MessageBox MB_ICONSTOP|MB_OK "Windows could not create the ${APP_DISPLAY_NAME} lifecycle gate." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
    System::Call 'KERNEL32::WaitForSingleObject(p $1, i 0) i.r0'
    ${If} $0 == ${APP_WAIT_OBJECT_0}
    ${OrIf} $0 == ${APP_WAIT_ABANDONED}
        Nop
    ${ElseIf} $0 == ${APP_WAIT_TIMEOUT}
        System::Call 'KERNEL32::CloseHandle(p $1) i.r0'
        StrCpy $LifecycleMutexHandle 0
        !insertmacro ReleaseInstallerMutex
        MessageBox MB_ICONEXCLAMATION|MB_OK "Another ${APP_DISPLAY_NAME} lifecycle command is running." /SD IDOK
        SetErrorLevel ${APP_EXIT_LIFECYCLE_BUSY}
        Quit
    ${Else}
        System::Call 'KERNEL32::CloseHandle(p $1) i.r0'
        StrCpy $LifecycleMutexHandle 0
        !insertmacro ReleaseInstallerMutex
        MessageBox MB_ICONSTOP|MB_OK "Windows could not acquire the ${APP_DISPLAY_NAME} lifecycle gate." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
!macroend

!macro InitializeRuntimeCommon FAILURE_CODE
    StrCpy $InstallerMutexHandle 0
    StrCpy $LifecycleMutexHandle 0
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
    SetRegView 64
    SetShellVarContext current
!macroend

!macro InitializeSetupRuntime FAILURE_CODE
    !insertmacro InitializeRuntimeCommon ${FAILURE_CODE}
    ${If} $LOCALAPPDATA == ""
        MessageBox MB_ICONSTOP|MB_OK "Windows did not provide a current-user LocalAppData directory." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
!macroend

!macro InitializeUninstallRuntime FAILURE_CODE
    !insertmacro InitializeRuntimeCommon ${FAILURE_CODE}
    ; Preserve the path established by the NSIS uninstaller bootstrap or the
    ; documented _?= override. It is later bound to the registered location and
    ; product GUID before any mutation.
    ${If} $INSTDIR == ""
        MessageBox MB_ICONSTOP|MB_OK "Windows did not provide the uninstaller installation directory." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
!macroend

Function OpenInstalledConfiguration
    IfSilent done
    ${If} $ExistingInstallation == "1"
        Goto done
    ${EndIf}
    ; Installation is already committed and section-owned gates are released.
    SetOutPath "$INSTDIR"
    nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-open-configuration'
    Pop $0
    Pop $1
    SetOutPath "$TEMP"
    ${If} $0 == "error"
        MessageBox MB_ICONEXCLAMATION|MB_OK "Windows could not open ${APP_DISPLAY_NAME} configuration. Run ${APP_NAME} config from a new terminal." /SD IDOK
    ${ElseIf} $0 != "0"
        MessageBox MB_ICONEXCLAMATION|MB_OK "Configuration opening returned status $0. $1 Run ${APP_NAME} config from a new terminal." /SD IDOK
    ${EndIf}
    done:
FunctionEnd

Function .onInit
    !insertmacro InitializeSetupRuntime ${APP_EXIT_INSTALL_FAILED}
    StrCpy $InstallMutationActive "0"
FunctionEnd

Function PreventInstallMutationAbort
    ${If} $InstallMutationActive == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is completing or rolling back installation changes. Wait for setup to finish." /SD IDOK
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
    GetDlgItem $R8 $HWNDPARENT 2
    EnableWindow $R8 0
    done:
FunctionEnd

Function EnableInstallCancellation
    IfSilent done
    GetDlgItem $R8 $HWNDPARENT 2
    EnableWindow $R8 1
    done:
FunctionEnd

Function un.DisableUninstallCancellation
    IfSilent done
    GetDlgItem $R8 $HWNDPARENT 2
    EnableWindow $R8 0
    done:
FunctionEnd

Function un.onInit
    !insertmacro InitializeUninstallRuntime ${APP_EXIT_UNINSTALL_FAILED}
    StrCpy $DeleteConfigurationOnUninstall "0"
    StrCpy $DeleteConfigurationUnavailable "0"
    StrCpy $DeleteConfigurationCheckbox ""
    StrCpy $InstallMutationActive "0"
    ; Match /DELETE_CONFIG as a complete whitespace-delimited token. Padding
    ; prevents suffixes and path fragments from enabling destructive cleanup.
    ${GetParameters} $0
    StrCpy $0 " $0 "
    ClearErrors
    ${GetOptions} $0 " /DELETE_CONFIG " $1
    ${IfNot} ${Errors}
        StrCpy $DeleteConfigurationOnUninstall "1"
    ${EndIf}
FunctionEnd

Function un.DeleteConfigurationPage
    IfSilent done 0
    StrCpy $DeleteConfigurationUnavailable "0"
    StrCpy $DeleteConfigurationCheckbox ""

    ; A retry after successful application cleanup may have no executable left.
    ; In that state the remaining installer cleanup can continue, but it cannot
    ; newly perform the optional application-owned configuration deletion.
    ClearErrors
    ReadRegDWORD $1 HKCU "${UNINSTALL_KEY}" "CleanupComplete"
    ${IfNot} ${Errors}
    ${AndIf} $1 == "1"
        System::Call 'KERNEL32::SetLastError(i 0)'
        System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR\${APP_EXECUTABLE}") i.r1 ?e'
        Pop $2
        ${If} $1 == -1
            ${If} $2 == ${APP_ERROR_FILE_NOT_FOUND}
            ${OrIf} $2 == ${APP_ERROR_PATH_NOT_FOUND}
                StrCpy $DeleteConfigurationUnavailable "1"
            ${EndIf}
        ${EndIf}
    ${EndIf}

    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}

    ${If} $DeleteConfigurationUnavailable == "1"
        ${NSD_CreateLabel} 0 0 100% 96u "Application cleanup completed during an earlier uninstall attempt. This retry can remove the remaining installer files, but it cannot newly delete ${APP_CONFIG_FILE} or ${APP_USER_SCRIPT} because the application executable is already gone. Run setup once and uninstall again only if those optional files must also be deleted. Project ${APP_PROJECT_DIRECTORY} profiles are not removed."
        Pop $0
    ${Else}
        ${NSD_CreateLabel} 0 0 100% 72u "Uninstall removes ${APP_DISPLAY_NAME} machine-local state, SSH integration, and the configured package/tool cache. A running Sandbox stays open but becomes unmanaged; close it manually when finished. Select this option to also remove ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}. Project ${APP_PROJECT_DIRECTORY} profiles are not removed."
        Pop $0
        ${NSD_CreateCheckbox} 0 82u 100% 14u "Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}"
        Pop $DeleteConfigurationCheckbox
        ${If} $DeleteConfigurationOnUninstall == "1"
            ${NSD_Check} $DeleteConfigurationCheckbox
        ${EndIf}
    ${EndIf}
    nsDialogs::Show
    done:
FunctionEnd

Function un.DeleteConfigurationPageLeave
    IfSilent done 0
    ${If} $DeleteConfigurationCheckbox != ""
        ${NSD_GetState} $DeleteConfigurationCheckbox $0
        ${If} $0 == ${BST_CHECKED}
            StrCpy $DeleteConfigurationOnUninstall "1"
        ${Else}
            StrCpy $DeleteConfigurationOnUninstall "0"
        ${EndIf}
    ${EndIf}
    done:
FunctionEnd

!macro DefineDirectoryCheck PREFIX
Function ${PREFIX}CheckInstallDirectory
    StrCpy $InstallDirectorySafe "1"
    StrCpy $InstallDirectoryExists "0"
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::GetFileAttributesW(w "$INSTDIR") i.r0 ?e'
    Pop $2
    ${If} $0 == -1
        ${If} $2 != ${APP_ERROR_FILE_NOT_FOUND}
        ${AndIf} $2 != ${APP_ERROR_PATH_NOT_FOUND}
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
    ${Else}
        StrCpy $InstallDirectoryExists "1"
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $1 == 0
        ${OrIf} $2 != 0
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
    ${EndIf}
FunctionEnd
!macroend

!macro DefineUninstallKeyStateCheck PREFIX
Function ${PREFIX}CheckUninstallKeyState
    StrCpy $UninstallKeyState ${APP_REGISTRY_KEY_STATE_UNSAFE}
    System::Call 'ADVAPI32::RegOpenKeyExW(p ${APP_HKEY_CURRENT_USER}, w "${UNINSTALL_KEY}", i 0, i ${APP_KEY_READ_64}, *p .r0) i.r1'
    ${If} $1 == ${APP_ERROR_SUCCESS}
        System::Call 'ADVAPI32::RegCloseKey(p r0) i.r1'
        StrCpy $UninstallKeyState ${APP_REGISTRY_KEY_STATE_PRESENT}
    ${ElseIf} $1 == ${APP_ERROR_FILE_NOT_FOUND}
    ${OrIf} $1 == ${APP_ERROR_PATH_NOT_FOUND}
        StrCpy $UninstallKeyState ${APP_REGISTRY_KEY_STATE_ABSENT}
    ${EndIf}
FunctionEnd
!macroend

!macro GetRegistryValueState NAME
    StrCpy $RegistryValueState ${APP_REGISTRY_VALUE_STATE_UNSAFE}
    StrCpy $RegistryValueType 0
    System::Call 'ADVAPI32::RegOpenKeyExW(p ${APP_HKEY_CURRENT_USER}, w "${UNINSTALL_KEY}", i 0, i ${APP_KEY_READ_64}, *p .r0) i.r1'
    ${If} $1 == ${APP_ERROR_SUCCESS}
        StrCpy $2 0
        StrCpy $3 0
        System::Call 'ADVAPI32::RegQueryValueExW(p r0, w "${NAME}", p 0, *i .r3, p 0, *i .r2) i.r1'
        System::Call 'ADVAPI32::RegCloseKey(p r0) i.r0'
        ${If} $1 == ${APP_ERROR_SUCCESS}
            StrCpy $RegistryValueState ${APP_REGISTRY_VALUE_STATE_PRESENT}
            StrCpy $RegistryValueType $3
        ${ElseIf} $1 == ${APP_ERROR_FILE_NOT_FOUND}
        ${OrIf} $1 == ${APP_ERROR_PATH_NOT_FOUND}
            StrCpy $RegistryValueState ${APP_REGISTRY_VALUE_STATE_ABSENT}
        ${EndIf}
    ${ElseIf} $1 == ${APP_ERROR_FILE_NOT_FOUND}
    ${OrIf} $1 == ${APP_ERROR_PATH_NOT_FOUND}
        StrCpy $RegistryValueState ${APP_REGISTRY_VALUE_STATE_ABSENT}
    ${EndIf}
!macroend

!macro SnapshotRegistryValue NAME PRESENT TYPE VALUE
    StrCpy ${PRESENT} "0"
    StrCpy ${TYPE} 0
    StrCpy ${VALUE} ""
    !insertmacro GetRegistryValueState "${NAME}"
    ${If} $RegistryValueState == ${APP_REGISTRY_VALUE_STATE_PRESENT}
        StrCpy ${TYPE} $RegistryValueType
        ${If} ${TYPE} == ${APP_REGISTRY_TYPE_SZ}
        ${OrIf} ${TYPE} == ${APP_REGISTRY_TYPE_EXPAND_SZ}
            ClearErrors
            ReadRegStr ${VALUE} HKCU "${UNINSTALL_KEY}" "${NAME}"
            ${If} ${Errors}
                Goto install_snapshot_failure
            ${EndIf}
        ${ElseIf} ${TYPE} == ${APP_REGISTRY_TYPE_DWORD}
            ClearErrors
            ReadRegDWORD ${VALUE} HKCU "${UNINSTALL_KEY}" "${NAME}"
            ${If} ${Errors}
                Goto install_snapshot_failure
            ${EndIf}
        ${Else}
            Goto install_snapshot_failure
        ${EndIf}
        StrCpy ${PRESENT} "1"
    ${ElseIf} $RegistryValueState != ${APP_REGISTRY_VALUE_STATE_ABSENT}
        Goto install_snapshot_failure
    ${EndIf}
!macroend

!macro RestoreRegistryValue NAME PRESENT TYPE VALUE
    ${If} ${PRESENT} == "1"
        ClearErrors
        ${If} ${TYPE} == ${APP_REGISTRY_TYPE_SZ}
            WriteRegStr HKCU "${UNINSTALL_KEY}" "${NAME}" "${VALUE}"
        ${ElseIf} ${TYPE} == ${APP_REGISTRY_TYPE_EXPAND_SZ}
            WriteRegExpandStr HKCU "${UNINSTALL_KEY}" "${NAME}" "${VALUE}"
        ${ElseIf} ${TYPE} == ${APP_REGISTRY_TYPE_DWORD}
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "${NAME}" ${VALUE}
        ${Else}
            StrCpy $RegistryRestoreFailed "1"
            SetErrors
        ${EndIf}
        ${If} ${Errors}
            StrCpy $RegistryRestoreFailed "1"
            ClearErrors
        ${Else}
            !insertmacro GetRegistryValueState "${NAME}"
            ${If} $RegistryValueState != ${APP_REGISTRY_VALUE_STATE_PRESENT}
            ${OrIf} $RegistryValueType != ${TYPE}
                StrCpy $RegistryRestoreFailed "1"
            ${ElseIf} ${TYPE} == ${APP_REGISTRY_TYPE_DWORD}
                ClearErrors
                ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "${NAME}"
                ${If} ${Errors}
                ${OrIf} $0 != ${VALUE}
                    StrCpy $RegistryRestoreFailed "1"
                ${EndIf}
            ${Else}
                ClearErrors
                ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "${NAME}"
                ${If} ${Errors}
                ${OrIf} $0 != ${VALUE}
                    StrCpy $RegistryRestoreFailed "1"
                ${EndIf}
            ${EndIf}
        ${EndIf}
    ${Else}
        ClearErrors
        DeleteRegValue HKCU "${UNINSTALL_KEY}" "${NAME}"
        !insertmacro GetRegistryValueState "${NAME}"
        ${If} $RegistryValueState != ${APP_REGISTRY_VALUE_STATE_ABSENT}
            StrCpy $RegistryRestoreFailed "1"
        ${EndIf}
    ${EndIf}
!macroend

!macro GetRegularFileState PATH
    StrCpy $FileState ${APP_FILE_STATE_UNSAFE}
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::GetFileAttributesW(w "${PATH}") i.r0 ?e'
    Pop $1
    ${If} $0 == -1
        ${If} $1 == ${APP_ERROR_FILE_NOT_FOUND}
        ${OrIf} $1 == ${APP_ERROR_PATH_NOT_FOUND}
            StrCpy $FileState ${APP_FILE_STATE_ABSENT}
        ${EndIf}
    ${Else}
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $1 == 0
        ${AndIf} $2 == 0
            StrCpy $FileState ${APP_FILE_STATE_REGULAR}
        ${EndIf}
    ${EndIf}
!macroend

!macro GetRegularDirectoryState PATH
    StrCpy $FileState ${APP_FILE_STATE_UNSAFE}
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::GetFileAttributesW(w "${PATH}") i.r0 ?e'
    Pop $1
    ${If} $0 == -1
        ${If} $1 == ${APP_ERROR_FILE_NOT_FOUND}
        ${OrIf} $1 == ${APP_ERROR_PATH_NOT_FOUND}
            StrCpy $FileState ${APP_FILE_STATE_ABSENT}
        ${EndIf}
    ${Else}
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $1 != 0
        ${AndIf} $2 == 0
            StrCpy $FileState ${APP_FILE_STATE_REGULAR}
        ${EndIf}
    ${EndIf}
!macroend

!macro DefineOwnedDirectoryRemoval PREFIX
Function ${PREFIX}RemoveOwnedDirectoryTree
    Exch $0
    Push $1
    Push $2
    Push $3
    Push $4
    Push $5
    Push $6
    Push $7

    ${If} $OperationResult != "0"
        Goto remove_done
    ${EndIf}
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::GetFileAttributesW(w "$0") i.r4 ?e'
    Pop $5
    ${If} $4 == -1
        ${If} $5 == ${APP_ERROR_FILE_NOT_FOUND}
        ${OrIf} $5 == ${APP_ERROR_PATH_NOT_FOUND}
            Goto remove_done
        ${EndIf}
        StrCpy $RootRemovalError $5
        StrCpy $OperationResult "1"
        Goto remove_done
    ${EndIf}
    IntOp $5 $4 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
    IntOp $6 $4 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
    ${If} $5 == 0
    ${OrIf} $6 != 0
        StrCpy $RootRemovalError -1
        StrCpy $OperationResult "1"
        Goto remove_done
    ${EndIf}

    ClearErrors
    FindFirst $1 $2 "$0\*"
    ${If} ${Errors}
        System::Call 'KERNEL32::GetLastError() i.r5'
        StrCpy $RootRemovalError $5
        StrCpy $OperationResult "1"
        Goto remove_done
    ${EndIf}
    remove_next:
        StrCmp $2 "" remove_enumerated
        StrCmp $2 "." remove_advance
        StrCmp $2 ".." remove_advance
        StrCpy $3 "$0\$2"
        System::Call 'KERNEL32::SetLastError(i 0)'
        System::Call 'KERNEL32::GetFileAttributesW(w "$3") i.r4 ?e'
        Pop $5
        ${If} $4 == -1
            ${If} $5 == ${APP_ERROR_FILE_NOT_FOUND}
            ${OrIf} $5 == ${APP_ERROR_PATH_NOT_FOUND}
                Goto remove_advance
            ${EndIf}
            StrCpy $RootRemovalError $5
            StrCpy $OperationResult "1"
            Goto remove_close
        ${EndIf}
        IntOp $5 $4 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $6 $4 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $5 != 0
            ${If} $6 != 0
                System::Call 'KERNEL32::SetLastError(i 0)'
                System::Call 'KERNEL32::RemoveDirectoryW(w "$3") i.r4 ?e'
                Pop $5
                ${If} $4 == 0
                    StrCpy $RootRemovalError $5
                    StrCpy $OperationResult "1"
                    Goto remove_close
                ${EndIf}
            ${Else}
                Push "$3"
                Call ${PREFIX}RemoveOwnedDirectoryTree
                ${If} $OperationResult != "0"
                    Goto remove_close
                ${EndIf}
            ${EndIf}
        ${Else}
            IntOp $6 $4 & ${APP_FILE_ATTRIBUTE_READONLY}
            ${If} $6 != 0
                IntOp $7 $4 & 0xFFFFFFFE
                System::Call 'KERNEL32::SetLastError(i 0)'
                System::Call 'KERNEL32::SetFileAttributesW(w "$3", i r7) i.r4 ?e'
                Pop $5
                ${If} $4 == 0
                    StrCpy $RootRemovalError $5
                    StrCpy $OperationResult "1"
                    Goto remove_close
                ${EndIf}
            ${EndIf}
            System::Call 'KERNEL32::SetLastError(i 0)'
            System::Call 'KERNEL32::DeleteFileW(w "$3") i.r4 ?e'
            Pop $5
            ${If} $4 == 0
                StrCpy $RootRemovalError $5
                ${If} $6 != 0
                    IntOp $7 $7 | ${APP_FILE_ATTRIBUTE_READONLY}
                    System::Call 'KERNEL32::SetFileAttributesW(w "$3", i r7) i.r4'
                ${EndIf}
                StrCpy $OperationResult "1"
                Goto remove_close
            ${EndIf}
        ${EndIf}
    remove_advance:
        ClearErrors
        FindNext $1 $2
        ${IfNot} ${Errors}
            Goto remove_next
        ${EndIf}
    remove_enumerated:
        FindClose $1
        System::Call 'KERNEL32::SetLastError(i 0)'
        System::Call 'KERNEL32::RemoveDirectoryW(w "$0") i.r4 ?e'
        Pop $5
        ${If} $4 == 0
            StrCpy $RootRemovalError $5
            StrCpy $OperationResult "1"
        ${EndIf}
        Goto remove_done
    remove_close:
        FindClose $1
    remove_done:
        Pop $7
        Pop $6
        Pop $5
        Pop $4
        Pop $3
        Pop $2
        Pop $1
        Pop $0
FunctionEnd
!macroend

!macro DefinePathHelper PREFIX
Function ${PREFIX}RunPathHelper
    InitPluginsDir
    SetOutPath "$PLUGINSDIR\control"
    ClearErrors
    File "/oname=path.ps1" "${PATH_HELPER}"
    ${If} ${Errors}
        StrCpy $0 "error"
        StrCpy $1 "Could not extract the PATH helper."
        Return
    ${EndIf}
    nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$PLUGINSDIR\control\path.ps1" -Action $PathAction -InstallDirectory "$INSTDIR"'
    Pop $0
    Pop $1
FunctionEnd
!macroend

!insertmacro DefineDirectoryCheck ""
!insertmacro DefineDirectoryCheck "un."
!insertmacro DefineUninstallKeyStateCheck ""
!insertmacro DefineUninstallKeyStateCheck "un."
!insertmacro DefineOwnedDirectoryRemoval ""
!insertmacro DefineOwnedDirectoryRemoval "un."

!insertmacro DefinePathHelper ""
!insertmacro DefinePathHelper "un."

!macro NotifyPathChanged
    System::Call 'USER32::SendMessageTimeoutW(p ${HWND_BROADCAST}, i ${WM_SETTINGCHANGE}, p 0, w "Environment", i 0x2, i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}, *p .r3) p.r2'
    ${If} $2 == 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "The PATH changed, but Windows did not confirm the refresh. Open a new terminal; sign out and back in if necessary." /SD IDOK
    ${EndIf}
!macroend

!macro BackupFile NAME
    !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        CopyFiles /SILENT "$INSTDIR\${NAME}" "$PLUGINSDIR\backup"
        ${If} ${Errors}
            StrCpy $BackupFailed "1"
        ${Else}
            !insertmacro GetRegularFileState "$PLUGINSDIR\backup\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_REGULAR}
                StrCpy $BackupFailed "1"
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        StrCpy $BackupFailed "1"
    ${EndIf}
!macroend

!macro InstallFile NAME
    ${If} $PayloadCopyFailed == "0"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\package\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not install ${NAME}."
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_REGULAR}
                StrCpy $PayloadCopyFailed "1"
                StrCpy $InstallFailureMessage "Installed ${NAME} is not a regular file."
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

!macro RestoreFile NAME
    !insertmacro GetRegularFileState "$PLUGINSDIR\backup\${NAME}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\backup\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $RollbackFailed "1"
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_REGULAR}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState == ${APP_FILE_STATE_ABSENT}
        !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
        ${If} $FileState == ${APP_FILE_STATE_REGULAR}
            ClearErrors
            Delete "$INSTDIR\${NAME}"
            ${If} ${Errors}
                StrCpy $RollbackFailed "1"
            ${Else}
                !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
                ${If} $FileState != ${APP_FILE_STATE_ABSENT}
                    StrCpy $RollbackFailed "1"
                ${EndIf}
            ${EndIf}
        ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
            StrCpy $RollbackFailed "1"
        ${EndIf}
    ${Else}
        StrCpy $RollbackFailed "1"
    ${EndIf}
!macroend

Function RestorePreInstallRegistryState
    StrCpy $RegistryRestoreFailed "0"
    ${If} $RegistryExistedBefore == "0"
        ; DeleteRegKey reports an error when the key is already absent. Check the
        ; actual state first and verify the desired absent result afterward.
        Call CheckUninstallKeyState
        ${If} $UninstallKeyState == ${APP_REGISTRY_KEY_STATE_ABSENT}
            Return
        ${ElseIf} $UninstallKeyState != ${APP_REGISTRY_KEY_STATE_PRESENT}
            StrCpy $RegistryRestoreFailed "1"
            Return
        ${EndIf}
        ClearErrors
        DeleteRegKey HKCU "${UNINSTALL_KEY}"
        Call CheckUninstallKeyState
        ${If} $UninstallKeyState != ${APP_REGISTRY_KEY_STATE_ABSENT}
            StrCpy $RegistryRestoreFailed "1"
        ${EndIf}
        Return
    ${EndIf}

    !insertmacro RestoreRegistryValue "ProductGuid" $ProductGuidWasPresent $ProductGuidOriginalType $ProductGuidOriginal
    !insertmacro RestoreRegistryValue "InstallLocation" $InstallLocationWasPresent $InstallLocationOriginalType $InstallLocationOriginal
    !insertmacro RestoreRegistryValue "InstallComplete" $InstallCompleteWasPresent $InstallCompleteOriginalType $InstallCompleteOriginal
    !insertmacro RestoreRegistryValue "InstallIntentToken" $InstallIntentTokenWasPresent $InstallIntentTokenOriginalType $InstallIntentTokenOriginal

    ; A pre-existing key must still exist after exact value restoration. The
    ; installer never intentionally removes it on this path.
    Call CheckUninstallKeyState
    ${If} $UninstallKeyState != ${APP_REGISTRY_KEY_STATE_PRESENT}
        StrCpy $RegistryRestoreFailed "1"
    ${EndIf}
FunctionEnd

Function EnsureInstallRepairRegistration
    StrCpy $RegistryRestoreFailed "0"
    ; InstallIntentToken is the single atomic ownership anchor for a setup that
    ; might stop before the remaining repair registration has been written.
    ; It binds the product and exact per-user install path.
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallIntentToken" "${APP_PRODUCT_GUID}|$INSTDIR"
    ${If} ${Errors}
        Goto ensure_repair_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    ${If} ${Errors}
        Goto ensure_repair_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    ${If} ${Errors}
        Goto ensure_repair_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
    ${If} ${Errors}
        Goto ensure_repair_registry_failed
    ${EndIf}
    Return

    ensure_repair_registry_failed:
        StrCpy $RegistryRestoreFailed "1"
FunctionEnd

Function un.EnsureRetryUninstaller
    StrCpy $OperationResult "1"
    !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        StrCpy $OperationResult "0"
        Return
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        Return
    ${EndIf}

    !insertmacro GetRegularFileState "$EXEPATH"
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        Return
    ${EndIf}
    System::Call 'KERNEL32::CopyFileW(w "$EXEPATH", w "$INSTDIR\uninstall.exe", i 1) i.r0'
    ${If} $0 == 0
        Return
    ${EndIf}
    !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        StrCpy $OperationResult "0"
    ${EndIf}
FunctionEnd

Function un.RestoreRetryRegistration
    StrCpy $RegistryRestoreFailed "0"

    !insertmacro GetRegularDirectoryState "$INSTDIR"
    ${If} $FileState == ${APP_FILE_STATE_ABSENT}
        ClearErrors
        CreateDirectory "$INSTDIR"
        ${If} ${Errors}
            Goto restore_registry_failed
        ${EndIf}
        !insertmacro GetRegularDirectoryState "$INSTDIR"
    ${EndIf}
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        Goto restore_registry_failed
    ${EndIf}

    !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
    ${If} $FileState == ${APP_FILE_STATE_ABSENT}
        !insertmacro GetRegularFileState "$EXEPATH"
        ${If} $FileState == ${APP_FILE_STATE_REGULAR}
            System::Call 'KERNEL32::CopyFileW(w "$EXEPATH", w "$INSTDIR\uninstall.exe", i 0) i.r0'
            ${If} $0 == 0
                Goto restore_registry_failed
            ${EndIf}
            !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
        ${EndIf}
    ${EndIf}
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        Goto restore_registry_failed
    ${EndIf}

    !insertmacro GetRegularFileState "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        StrCpy $9 "1"
    ${Else}
        ; Quiet uninstall is optional for recovery. A normal installed
        ; uninstall.exe is sufficient to retry or repair the installation.
        StrCpy $9 "0"
    ${EndIf}

    ; InstallIntentToken is the first retry-registry write. If any later write
    ; fails or the process stops, matching setup can repair the partial key from
    ; this exact product and location binding.
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallIntentToken" "${APP_PRODUCT_GUID}|$INSTDIR"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}

    ; Build a deliberately incomplete retry registration. Every required value
    ; is checked individually so no earlier failure can be hidden by a later
    ; successful registry call.
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\uninstall.exe",0'
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ${If} $9 == "1"
        ClearErrors
        WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}" -Uninstaller "$INSTDIR\uninstall.exe" -InstallDirectory "$INSTDIR"'
        ${If} ${Errors}
            Goto restore_registry_failed
        ${EndIf}
    ${Else}
        ClearErrors
        DeleteRegValue HKCU "${UNINSTALL_KEY}" "QuietUninstallString"
        ClearErrors
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" 0
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}

    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    ${If} ${Errors}
        Goto restore_registry_failed
    ${EndIf}

    ; Keep InstallIntentToken and InstallComplete=0 so setup, rather than an
    ; incomplete binary root, owns the only supported repair path.
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "DisplayVersion"
    ClearErrors
    Return

    restore_registry_failed:
        StrCpy $RegistryRestoreFailed "1"
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
FunctionEnd

Section "Install"
    !insertmacro AcquireInstallerMutex ${APP_EXIT_INSTALL_FAILED}
    !insertmacro AcquireLifecycleMutex ${APP_EXIT_INSTALL_FAILED}

    StrCpy $ExistingInstallation "0"
    StrCpy $ExistingRegistryOwned "0"
    StrCpy $InstallCompleteWasPresent "0"
    StrCpy $InstallCompleteOriginal "0"
    StrCpy $InstallCompleteOriginalType 0
    StrCpy $ProductGuidWasPresent "0"
    StrCpy $ProductGuidOriginal ""
    StrCpy $ProductGuidOriginalType 0
    StrCpy $InstallLocationWasPresent "0"
    StrCpy $InstallLocationOriginal ""
    StrCpy $InstallLocationOriginalType 0
    StrCpy $InstallIntentTokenWasPresent "0"
    StrCpy $InstallIntentTokenOriginal ""
    StrCpy $InstallIntentTokenOriginalType 0
    StrCpy $InstallIntentTokenValid "0"
    StrCpy $RegistryExistedBefore "0"
    StrCpy $InstallDirectoryExistedBefore "0"
    StrCpy $InstallDirectoryCreatedThisRun "0"
    StrCpy $PathOwned "0"
    StrCpy $PathPending "0"
    StrCpy $PathNotificationRequired "0"
    StrCpy $BackupFailed "0"
    StrCpy $PayloadCopyFailed "0"
    StrCpy $RollbackFailed "0"
    StrCpy $RegistryRestoreFailed "0"
    StrCpy $InstallFailureMessage ""

    Call CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The fixed install path is not a regular directory." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}
    StrCpy $InstallDirectoryExistedBefore $InstallDirectoryExists

    StrCpy $InstallDirectoryNonEmpty "0"
    ${If} $InstallDirectoryExists == "1"
        ${DirState} "$INSTDIR" $InstallDirectoryNonEmpty
        ${If} $InstallDirectoryNonEmpty == "-1"
            MessageBox MB_ICONSTOP|MB_OK "The existing install directory could not be enumerated safely. No files were changed." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
        ${EndIf}
    ${EndIf}

    Call CheckUninstallKeyState
    ${If} $UninstallKeyState == ${APP_REGISTRY_KEY_STATE_UNSAFE}
        MessageBox MB_ICONSTOP|MB_OK "The fixed product registry key is inaccessible. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}
    ${If} $UninstallKeyState == ${APP_REGISTRY_KEY_STATE_PRESENT}
        StrCpy $RegistryExistedBefore "1"
    ${EndIf}

    ; Snapshot only the values changed before payload commit so preparation and
    ; rollback can restore exact prior value presence, type, and data.
    ${If} $UninstallKeyState == ${APP_REGISTRY_KEY_STATE_PRESENT}
        !insertmacro SnapshotRegistryValue "ProductGuid" $ProductGuidWasPresent $ProductGuidOriginalType $ProductGuidOriginal
        !insertmacro SnapshotRegistryValue "InstallLocation" $InstallLocationWasPresent $InstallLocationOriginalType $InstallLocationOriginal
        !insertmacro SnapshotRegistryValue "InstallComplete" $InstallCompleteWasPresent $InstallCompleteOriginalType $InstallCompleteOriginal
        !insertmacro SnapshotRegistryValue "InstallIntentToken" $InstallIntentTokenWasPresent $InstallIntentTokenOriginalType $InstallIntentTokenOriginal

        ${If} $InstallIntentTokenWasPresent == "1"
        ${AndIf} $InstallIntentTokenOriginalType == ${APP_REGISTRY_TYPE_SZ}
        ${AndIf} $InstallIntentTokenOriginal == "${APP_PRODUCT_GUID}|$INSTDIR"
            StrCpy $InstallIntentTokenValid "1"
        ${EndIf}
    ${EndIf}

    Goto install_snapshot_complete
    install_snapshot_failure:
        MessageBox MB_ICONSTOP|MB_OK "An existing product registry value has an unsupported type or cannot be snapshotted exactly. No files or registry values were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    install_snapshot_complete:

    ${If} $UninstallKeyState == ${APP_REGISTRY_KEY_STATE_PRESENT}
        ClearErrors
        ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
        ${IfNot} ${Errors}
            ${If} $0 != "${APP_PRODUCT_GUID}"
                MessageBox MB_ICONSTOP|MB_OK "The fixed product key belongs to another product." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}

            ClearErrors
            ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
            ${IfNot} ${Errors}
                ${If} $0 != "$INSTDIR"
                    MessageBox MB_ICONSTOP|MB_OK "The registered install location does not match the fixed location." /SD IDOK
                    SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                    !insertmacro ReleaseAllMutexes
                    Quit
                ${EndIf}
            ${ElseIf} $InstallIntentTokenValid != "1"
                MessageBox MB_ICONSTOP|MB_OK "The registered install location is missing and no valid installation intent can repair it." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}

        ${Else}
            ; InstallIntentToken is written atomically before every other repair
            ; value. It is therefore the only accepted ownership proof when a
            ; crash left the key before ProductGuid was committed.
            ${If} $InstallIntentTokenValid != "1"
                MessageBox MB_ICONSTOP|MB_OK "The fixed product key is missing the matching ProductGuid and has no valid installation intent." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}

            ; Any partial values that survived must agree with the token. Missing
            ; values are completed by EnsureInstallRepairRegistration.
            ClearErrors
            ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
            ${IfNot} ${Errors}
            ${AndIf} $0 != "$INSTDIR"
                MessageBox MB_ICONSTOP|MB_OK "The partial installation intent points to another location." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}
            ${If} $InstallCompleteWasPresent == "1"
            ${AndIf} $InstallCompleteOriginal != "0"
                MessageBox MB_ICONSTOP|MB_OK "The partial installation intent has an inconsistent completion state." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}
        ${EndIf}

        StrCpy $ExistingInstallation "1"
        StrCpy $ExistingRegistryOwned "1"
    ${Else}
        ${If} $InstallDirectoryNonEmpty == "1"
            MessageBox MB_ICONSTOP|MB_OK "The fixed install directory is nonempty but has no matching product registration. Its contents were preserved." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
        ${EndIf}
    ${EndIf}

    ${If} $ExistingRegistryOwned == "1"
        ClearErrors
        ReadRegDWORD $PathOwned HKCU "${UNINSTALL_KEY}" "PathAdded"
        ${If} ${Errors}
        ${OrIf} $PathOwned != "1"
            StrCpy $PathOwned "0"
        ${EndIf}
        ClearErrors
        ReadRegDWORD $PathPending HKCU "${UNINSTALL_KEY}" "PathAddPending"
        ${If} ${Errors}
        ${OrIf} $PathPending != "1"
            StrCpy $PathPending "0"
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
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the complete package. No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the staged uninstaller. No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ; Installed-state mutation starts with creation of the fixed root. Keep
    ; interactive cancellation disabled through commit or complete rollback.
    StrCpy $InstallMutationActive "1"
    Call DisableInstallCancellation

    ClearErrors
    CreateDirectory "$PLUGINSDIR\backup"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Could not create the private backup directory."
        Goto install_preparation_failure
    ${EndIf}

    ; Persist the atomic repair token before the fixed target root can be created
    ; or changed. Every crash prefix that can leave target state now also leaves
    ; exact registry ownership evidence.
    Call EnsureInstallRepairRegistration
    ${If} $RegistryRestoreFailed == "1"
        StrCpy $InstallFailureMessage "Could not establish the installation recovery intent."
        Call RestorePreInstallRegistryState
        Goto install_preparation_failure
    ${EndIf}

    ${If} $InstallDirectoryExistedBefore == "0"
        ; Create the parent normally, then create the product leaf atomically. If
        ; the leaf appeared after preflight, preserve it and abort instead of
        ; assuming ownership of another actor's directory.
        ClearErrors
        CreateDirectory "$LOCALAPPDATA\Programs"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not create the per-user Programs directory."
            Goto install_preparation_failure
        ${EndIf}
        System::Call 'KERNEL32::GetFileAttributesW(w "$LOCALAPPDATA\Programs") i.r0'
        IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $0 == -1
        ${OrIf} $1 == 0
        ${OrIf} $2 != 0
            StrCpy $InstallFailureMessage "The per-user Programs directory is inaccessible or a reparse point."
            Goto install_preparation_failure
        ${EndIf}

        System::Call 'KERNEL32::SetLastError(i 0)'
        System::Call 'KERNEL32::CreateDirectoryW(w "$INSTDIR", p 0) i.r0 ?e'
        Pop $1
        ${If} $0 != 0
            StrCpy $InstallDirectoryCreatedThisRun "1"
        ${ElseIf} $1 == ${APP_ERROR_ALREADY_EXISTS}
            StrCpy $InstallFailureMessage "The install directory appeared after ownership preflight. Its contents were preserved."
            Goto install_preparation_failure
        ${Else}
            StrCpy $InstallFailureMessage "Could not create the install directory (Windows error $1)."
            Goto install_preparation_failure
        ${EndIf}
    ${EndIf}

    ; Recheck type and emptiness after creation. A previously unowned root must
    ; still be empty immediately before the fixed product claims it.
    Call CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        StrCpy $InstallFailureMessage "The install directory changed into an unsafe path before payload preparation."
        Goto install_preparation_failure
    ${EndIf}
    ${If} $ExistingInstallation == "0"
    ${OrIf} $InstallDirectoryCreatedThisRun == "1"
        ${DirState} "$INSTDIR" $InstallDirectoryNonEmpty
        ${If} $InstallDirectoryNonEmpty != "0"
            StrCpy $InstallFailureMessage "The unowned install directory became nonempty during setup. Its contents were preserved."
            Goto install_preparation_failure
        ${EndIf}
    ${EndIf}

    !insertmacro BackupFile "${APP_BASE_SCRIPT}"
    !insertmacro BackupFile "${APP_LICENSE}"
    !insertmacro BackupFile "${APP_STACK_SCRIPT}"
    !insertmacro BackupFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro BackupFile "uninstall.exe"
    !insertmacro BackupFile "${APP_EXECUTABLE}"
    ${If} $BackupFailed == "1"
        StrCpy $InstallFailureMessage "Could not back up every existing installer-owned file."
        Goto install_preparation_failure
    ${EndIf}

    ; A matching current registration or repair intent owns the complete binary
    ; root. Replace that root as one unit so stale and unknown files cannot survive
    ; an upgrade. The private backup retains only the previous supported payload.
    ${If} $ExistingInstallation == "1"
        SetOutPath "$TEMP"
        StrCpy $OperationResult "0"
        StrCpy $RootRemovalError 0
        Push "$INSTDIR"
        Call RemoveOwnedDirectoryTree
        Call CheckInstallDirectory
        ${If} $OperationResult != "0"
        ${OrIf} $InstallDirectorySafe != "1"
        ${OrIf} $InstallDirectoryExists != "0"
            ${If} $RootRemovalError == ${APP_ERROR_ACCESS_DENIED}
            ${OrIf} $RootRemovalError == ${APP_ERROR_SHARING_VIOLATION}
            ${OrIf} $RootRemovalError == ${APP_ERROR_LOCK_VIOLATION}
                StrCpy $InstallFailureMessage "A ${APP_DISPLAY_NAME} file is still in use. Close every running ${APP_DISPLAY_NAME} command, then run setup again."
            ${ElseIf} $RootRemovalError == -1
                StrCpy $InstallFailureMessage "The installer-owned binary directory changed into an unsafe path. No further files were removed."
            ${Else}
                StrCpy $InstallFailureMessage "Could not remove the previous installer-owned binary directory (Windows error $RootRemovalError)."
            ${EndIf}
            Goto install_rollback
        ${EndIf}
        ClearErrors
        CreateDirectory "$INSTDIR"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not recreate the installer-owned binary directory."
            Goto install_rollback
        ${EndIf}
        Call CheckInstallDirectory
        ${If} $InstallDirectorySafe != "1"
        ${OrIf} $InstallDirectoryExists != "1"
            StrCpy $InstallFailureMessage "The recreated installer-owned binary directory is unsafe."
            Goto install_rollback
        ${EndIf}
    ${EndIf}

    !insertmacro InstallFile "${APP_BASE_SCRIPT}"
    !insertmacro InstallFile "${APP_LICENSE}"
    !insertmacro InstallFile "${APP_STACK_SCRIPT}"
    !insertmacro InstallFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro InstallFile "uninstall.exe"
    !insertmacro InstallFile "${APP_EXECUTABLE}"

    ${If} $PayloadCopyFailed == "1"
        Goto install_rollback
    ${EndIf}

    ; Canonicalize the current-user PATH under a durable retry intent. The helper
    ; is idempotent: it removes empty and duplicate entries, keeps exactly one
    ; canonical product entry, and preserves the first unrelated occurrence so
    ; command precedence does not change. PathAdded now means that this installer
    ; manages the product entry, not that it can prove which historical process
    ; first wrote an equivalent string.
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAddPending" 1
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "PATH reconciliation intent could not be recorded."
        Goto install_integration_failure
    ${EndIf}

    StrCpy $PathAction "Add"
    Call RunPathHelper
    ${If} $0 != "0"
    ${AndIf} $0 != "10"
        StrCpy $InstallFailureMessage "PATH reconciliation failed: status $0. $1"
        Goto install_integration_failure
    ${EndIf}

    ; A previous pending intent means an earlier helper may have changed PATH and
    ; stopped before broadcasting. A current result of 10 means this run changed it.
    ${If} $0 == "10"
        StrCpy $PathNotificationRequired "1"
    ${ElseIf} $PathPending == "1"
        StrCpy $PathNotificationRequired "1"
    ${EndIf}

    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" 1
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Managed PATH state could not be recorded."
        Goto install_integration_failure
    ${EndIf}
    StrCpy $PathOwned "1"

    ; Broadcast before clearing the durable intent. If setup is terminated before
    ; or during notification, the next repair sees PathAddPending and retries it.
    ${If} $PathNotificationRequired == "1"
        !insertmacro NotifyPathChanged
    ${EndIf}

    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "PATH reconciliation intent could not be cleared."
        Goto install_integration_failure
    ${EndIf}
    StrCpy $PathPending "0"

    ; Write non-visible integration state while InstallComplete remains zero.
    ; The error flag deliberately accumulates across this fixed batch.
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\${APP_EXECUTABLE}",0'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}" -Uninstaller "$INSTDIR\uninstall.exe" -InstallDirectory "$INSTDIR"'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $PathOwned
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Windows Installed Apps registration failed."
        Goto install_integration_failure
    ${EndIf}

    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "ProductGuid could not be committed."
        Goto install_integration_failure
    ${EndIf}

    ; The complete payload is now valid, so the transient recovery intent can be
    ; cleared before the final registration commit.
    ClearErrors
    DeleteRegValue HKCU "${UNINSTALL_KEY}" "InstallIntentToken"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Installation recovery intent could not be cleared."
        Goto install_integration_failure
    ${EndIf}

    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "InstallComplete could not be committed."
        Goto install_integration_failure
    ${EndIf}

    ; DisplayVersion is the final Windows/WinGet-visible commit.
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    ${If} ${Errors}
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
        StrCpy $InstallFailureMessage "DisplayVersion could not be committed."
        Goto install_integration_failure
    ${EndIf}

    DetailPrint "Creating default configuration when missing..."
    ; Setup remains serialized, but the application command may acquire its
    ; normal lifecycle gate.
    !insertmacro ReleaseLifecycleMutex
    SetOutPath "$INSTDIR"
    nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-seed-configuration'
    Pop $0
    Pop $1
    SetOutPath "$TEMP"
    ${If} $0 == "error"
        MessageBox MB_ICONEXCLAMATION|MB_OK "Installation succeeded, but configuration creation could not start. The application will retry when needed." /SD IDOK
    ${ElseIf} $0 != "0"
        MessageBox MB_ICONEXCLAMATION|MB_OK "Installation succeeded, but configuration creation returned status $0. $1 The application will retry when needed." /SD IDOK
    ${EndIf}
    Goto install_done

    install_preparation_failure:
        ; No payload file was replaced. Remove a root created by this run and
        ; restore the exact prior registration snapshot.
        StrCpy $RollbackFailed "0"
        ${If} $InstallDirectoryCreatedThisRun == "1"
            Call CheckInstallDirectory
            ${If} $InstallDirectorySafe == "1"
                ClearErrors
                RMDir "$INSTDIR"
                ${If} ${Errors}
                    StrCpy $RollbackFailed "1"
                ${EndIf}
            ${Else}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}

        Call RestorePreInstallRegistryState
        ${If} $RegistryRestoreFailed == "1"
            StrCpy $RollbackFailed "1"
        ${EndIf}

        StrCpy $InstallMutationActive "0"
        Call EnableInstallCancellation
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage No installed payload file was changed, and the prior registry state was restored." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage No installed payload file was changed, but a directory or registry residual could not be removed." /SD IDOK
        ${EndIf}
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit

    install_integration_failure:
        ; Keep the complete installer-owned payload in an explicit repair state.
        Call EnsureInstallRepairRegistration
        ${If} $RegistryRestoreFailed == "1"
            StrCpy $InstallFailureMessage "$InstallFailureMessage The repair registration could not be completed."
        ${EndIf}
        StrCpy $InstallMutationActive "0"
        Call EnableInstallCancellation
        MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The complete payload remains installed. Run setup again to repair integration." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit

    install_rollback:
        StrCpy $RollbackFailed "0"
        !insertmacro RestoreFile "${APP_BASE_SCRIPT}"
        !insertmacro RestoreFile "${APP_LICENSE}"
        !insertmacro RestoreFile "${APP_STACK_SCRIPT}"
        !insertmacro RestoreFile "${APP_QUIET_UNINSTALL_HELPER}"
        !insertmacro RestoreFile "uninstall.exe"
        !insertmacro RestoreFile "${APP_EXECUTABLE}"

        ${If} $RollbackFailed == "0"
            Call RestorePreInstallRegistryState
            ${If} $RegistryRestoreFailed == "1"
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}

        ${If} $RollbackFailed != "0"
            ; Make every surviving partial root explicitly repairable.
            Call EnsureInstallRepairRegistration
            ${If} $RegistryRestoreFailed == "1"
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}

        ; Only a directory absent before this fresh attempt may be removed.
        ${If} $ExistingInstallation == "0"
        ${AndIf} $InstallDirectoryCreatedThisRun == "1"
        ${AndIf} $RollbackFailed == "0"
            StrCpy $OperationResult "0"
            StrCpy $RootRemovalError 0
            Push "$INSTDIR"
            Call RemoveOwnedDirectoryTree
            ${If} $OperationResult != "0"
                Call EnsureInstallRepairRegistration
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}

        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The previous installer-owned state was restored." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Rollback was incomplete; run setup again before using the command." /SD IDOK
        ${EndIf}
        StrCpy $InstallMutationActive "0"
        Call EnableInstallCancellation
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit

    install_done:
        StrCpy $InstallMutationActive "0"
        !insertmacro ReleaseAllMutexes
SectionEnd

Section "Uninstall"
    !insertmacro AcquireInstallerMutex ${APP_EXIT_UNINSTALL_FAILED}
    !insertmacro AcquireLifecycleMutex ${APP_EXIT_UNINSTALL_FAILED}

    SetAutoClose true
    SetOutPath "$TEMP"
    StrCpy $CleanupComplete "0"
    StrCpy $CleanupRetryRequired "0"
    StrCpy $RegistryRestoreFailed "0"
    StrCpy $InstallFailureMessage ""

    ; $INSTDIR is the path supplied by the NSIS uninstaller bootstrap or _?=.
    ; Bind it to the exact registered installation before any cleanup.
    Call un.CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The uninstaller path is not a regular directory." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    Call un.CheckUninstallKeyState
    ${If} $UninstallKeyState != ${APP_REGISTRY_KEY_STATE_PRESENT}
        MessageBox MB_ICONSTOP|MB_OK "The product registration is missing or inaccessible. No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${If} ${Errors}
    ${OrIf} $0 != "${APP_PRODUCT_GUID}"
        MessageBox MB_ICONSTOP|MB_OK "The product registration does not match this uninstaller." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
    ${If} ${Errors}
    ${OrIf} $0 != "$INSTDIR"
        MessageBox MB_ICONSTOP|MB_OK "The registered install location does not match this uninstaller." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
    ${If} ${Errors}
    ${OrIf} $0 != "1"
        MessageBox MB_ICONSTOP|MB_OK "Run the matching setup once to repair the incomplete installation before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ; A complete install must not retain an active payload-mutation intent.
    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallIntentToken"
    ${IfNot} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "The installation still carries an active repair intent. Run setup once before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ; Keep a normal retry entry point available before application cleanup.
    Call un.EnsureRetryUninstaller
    ${If} $OperationResult != "0"
        MessageBox MB_ICONSTOP|MB_OK "The installed uninstaller is missing or unsafe and could not be restored. Run the matching setup to repair it before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    ReadRegDWORD $CleanupComplete HKCU "${UNINSTALL_KEY}" "CleanupComplete"
    ${If} ${Errors}
    ${OrIf} $CleanupComplete != "1"
        StrCpy $CleanupComplete "0"
    ${EndIf}

    ; If an earlier attempt recorded cleanup but a runnable executable is still
    ; present, that attempt did not cross the safe deletion boundary.
    !insertmacro GetRegularFileState "$INSTDIR\${APP_EXECUTABLE}"
    StrCpy $9 $FileState

    ${If} $CleanupComplete == "1"
    ${AndIf} $9 == ${APP_FILE_STATE_ABSENT}
    ${AndIf} $DeleteConfigurationOnUninstall == "1"
        MessageBox MB_ICONSTOP|MB_OK "Optional configuration deletion was requested, but application cleanup was already committed and the application executable is no longer available. Run the matching setup once, then uninstall again with configuration deletion enabled." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ${If} $9 == ${APP_FILE_STATE_UNSAFE}
        MessageBox MB_ICONSTOP|MB_OK "A runnable installer-owned path is inaccessible or not a regular file. Run setup once to repair it before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ${If} $CleanupComplete == "1"
        ${If} $9 == ${APP_FILE_STATE_REGULAR}
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
            ${If} ${Errors}
                MessageBox MB_ICONSTOP|MB_OK "The retry cleanup state could not be reset. Run setup once to repair the installation, then uninstall again." /SD IDOK
                SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
                !insertmacro ReleaseAllMutexes
                Quit
            ${EndIf}
            StrCpy $CleanupComplete "0"
        ${EndIf}
    ${ElseIf} $9 != ${APP_FILE_STATE_REGULAR}
        MessageBox MB_ICONSTOP|MB_OK "The application executable required for cleanup is missing. Run setup once to repair the installation, then uninstall again." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}

    ClearErrors
    ReadRegDWORD $PathOwned HKCU "${UNINSTALL_KEY}" "PathAdded"
    ${If} ${Errors}
    ${OrIf} $PathOwned != "1"
        StrCpy $PathOwned "0"
    ${EndIf}

    ; Destructive mutation starts here. Keep cancellation disabled through every
    ; cleanup, PATH, registry and binary-directory decision.
    StrCpy $InstallMutationActive "1"
    Call un.DisableUninstallCancellation

    ${If} $CleanupComplete != "1"
        ; Keep the lifecycle gate continuously owned. The hidden command must
        ; honor --installer-lifecycle-lock-held and skip acquiring it itself.
        SetOutPath "$INSTDIR"
        ${If} $DeleteConfigurationOnUninstall == "1"
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-lifecycle-lock-held --delete-configuration'
        ${Else}
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-lifecycle-lock-held'
        ${EndIf}
        Pop $0
        Pop $1
        SetOutPath "$TEMP"
        ${If} $0 != "0"
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup failed with status $0. $1 No installer-owned files were removed." /SD IDOK
            StrCpy $InstallMutationActive "0"
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
        ${EndIf}

        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup succeeded but could not be recorded. Retrying is safe." /SD IDOK
            StrCpy $InstallMutationActive "0"
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
        ${EndIf}
        ; Until the application executable is gone, a retry must rerun
        ; cleanup because application state can be recreated between attempts.
        StrCpy $CleanupRetryRequired "1"
    ${EndIf}

    ; Remove every effective product entry and clean empty or duplicate PATH
    ; entries. The helper preserves the first unrelated occurrence, so command
    ; precedence is stable.
    StrCpy $PathAction "Remove"
    Call un.RunPathHelper
    ${If} $0 != "0"
    ${AndIf} $0 != "10"
        StrCpy $InstallFailureMessage "PATH cleanup failed with status $0. $1"
        Goto uninstall_retryable_failure
    ${EndIf}
    ${If} $0 == "10"
        !insertmacro NotifyPathChanged
    ${ElseIf} $PathOwned == "1"
        ; Recovery after a prior successful removal that stopped before broadcast.
        !insertmacro NotifyPathChanged
    ${EndIf}

    ; Registration and the exact fixed path have already proved ownership of the
    ; complete binary root. Remove the registration, then remove that root as one
    ; unit. A failure recreates a minimal repair entry point for setup.
    ClearErrors
    DeleteRegKey HKCU "${UNINSTALL_KEY}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Could not fully remove the Windows Installed Apps registration."
        Call un.RestoreRetryRegistration
        Goto uninstall_retryable_failure
    ${EndIf}

    SetOutPath "$TEMP"
    StrCpy $OperationResult "0"
    StrCpy $RootRemovalError 0
    Push "$INSTDIR"
    Call un.RemoveOwnedDirectoryTree
    Call un.CheckInstallDirectory
    ${If} $OperationResult != "0"
    ${OrIf} $InstallDirectorySafe != "1"
    ${OrIf} $InstallDirectoryExists != "0"
        ${If} $RootRemovalError == ${APP_ERROR_ACCESS_DENIED}
        ${OrIf} $RootRemovalError == ${APP_ERROR_SHARING_VIOLATION}
        ${OrIf} $RootRemovalError == ${APP_ERROR_LOCK_VIOLATION}
            StrCpy $InstallFailureMessage "A ${APP_DISPLAY_NAME} file is still in use. Close every running ${APP_DISPLAY_NAME} command, then run uninstall again."
        ${ElseIf} $RootRemovalError == -1
            StrCpy $InstallFailureMessage "The installer-owned binary directory changed into an unsafe path. No further files were removed."
        ${Else}
            StrCpy $InstallFailureMessage "Could not remove the installer-owned binary directory (Windows error $RootRemovalError)."
        ${EndIf}
        Call un.RestoreRetryRegistration
        Goto uninstall_retryable_failure
    ${EndIf}
    StrCpy $CleanupRetryRequired "0"
    Goto uninstall_done

    uninstall_retryable_failure:
        ${If} $RegistryRestoreFailed == "1"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage A repair entry point could not be restored. Run setup again." /SD IDOK
        ${ElseIf} $CleanupRetryRequired == "1"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
            ${If} ${Errors}
                MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Cleanup succeeded, but its retry state could not be reset. Run setup once to repair the installation, then uninstall again." /SD IDOK
            ${Else}
                MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The installation remains registered for repair. Run setup again before retrying uninstall." /SD IDOK
            ${EndIf}
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Run setup again before retrying uninstall." /SD IDOK
        ${EndIf}
        StrCpy $InstallMutationActive "0"
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit

    uninstall_done:
        StrCpy $InstallMutationActive "0"
        !insertmacro ReleaseAllMutexes
SectionEnd
