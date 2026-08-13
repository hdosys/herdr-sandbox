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
!insertmacro Require APP_INSTALLER_MARKER
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
!insertmacro ValidateLeaf APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
!insertmacro ValidateLeaf APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"

!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_BASE_SCRIPT "${APP_BASE_SCRIPT}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_EXECUTABLE "${APP_EXECUTABLE}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_BASE_SCRIPT "${APP_BASE_SCRIPT}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" APP_LICENSE "${APP_LICENSE}"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_STACK_SCRIPT "${APP_STACK_SCRIPT}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_LICENSE "${APP_LICENSE}" APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
!insertmacro AssertDifferent APP_LICENSE "${APP_LICENSE}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_LICENSE "${APP_LICENSE}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
!insertmacro AssertDifferent APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}" uninstall.exe "uninstall.exe"
!insertmacro AssertDifferent APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}" uninstall.exe "uninstall.exe"

; Optional, temporary migration from an old executable filename.
!ifdef APP_REPLACED_EXECUTABLE
    !if "${APP_REPLACED_EXECUTABLE}" == ""
        !undef APP_REPLACED_EXECUTABLE
    !else
        !insertmacro ValidateLeaf APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_EXECUTABLE "${APP_EXECUTABLE}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_BASE_SCRIPT "${APP_BASE_SCRIPT}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_STACK_SCRIPT "${APP_STACK_SCRIPT}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_LICENSE "${APP_LICENSE}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_INSTALLER_MARKER "${APP_INSTALLER_MARKER}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" APP_QUIET_UNINSTALL_HELPER "${APP_QUIET_UNINSTALL_HELPER}"
        !insertmacro AssertDifferent APP_REPLACED_EXECUTABLE "${APP_REPLACED_EXECUTABLE}" uninstall.exe "uninstall.exe"
    !endif
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "WinVer.nsh"
!include "x64.nsh"
!include "StrFunc.nsh"
${Using:StrFunc} StrRep

!define APP_INSTALLER_SCHEMA 1
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
!define APP_WAIT_OBJECT_0 0
!define APP_WAIT_ABANDONED 128
!define APP_WAIT_TIMEOUT 258
!define APP_FILE_ATTRIBUTE_DIRECTORY 0x10
!define APP_FILE_ATTRIBUTE_REPARSE_POINT 0x400
!define APP_FILE_STATE_ABSENT 0
!define APP_FILE_STATE_REGULAR 1
!define APP_FILE_STATE_UNSAFE 2
!define APP_EXIT_LIFECYCLE_BUSY 41
!define APP_EXIT_UNSUPPORTED_PLATFORM 50
!define APP_EXIT_INSTALL_FAILED 70
!define APP_EXIT_UNINSTALL_FAILED 80

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var InstallMutationActive
Var InstallerMutexHandle
Var LifecycleMutexHandle
Var ExistingInstallation
Var ExistingRegistryOwned
Var InstallCompleteWasComplete
Var OwnershipMarkerValid
Var InstallDirectorySafe
Var InstallDirectoryExists
Var InstallDirectoryNonEmpty
Var FileState
Var MarkerOperationResult
Var MarkerLineCount
Var MarkerLegacyState
Var MarkerLegacyInstallationId
Var MarkerLegacyOwnershipValid
Var MarkerLegacyRegistrationValid
Var RegistryRestoreFailed
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
Var InstallDirectoryHasUnknownEntries
Var UninstallResidual

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

!macro InitializeRuntime FAILURE_CODE
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
    ${If} $LOCALAPPDATA == ""
        MessageBox MB_ICONSTOP|MB_OK "Windows did not provide a current-user LocalAppData directory." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
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
    !insertmacro InitializeRuntime ${APP_EXIT_INSTALL_FAILED}
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
    !insertmacro InitializeRuntime ${APP_EXIT_UNINSTALL_FAILED}
    StrCpy $DeleteConfigurationOnUninstall "0"
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

Function CheckLegacyCanonicalGUID
    StrCpy $3 "0"
    StrLen $4 $2
    ${If} $4 != 36
        Return
    ${EndIf}
    ${ForEach} $4 0 35 + 1
        StrCpy $5 $2 1 $4
        ${If} $4 == 8
        ${OrIf} $4 == 13
        ${OrIf} $4 == 18
        ${OrIf} $4 == 23
            StrCmp $5 '-' legacy_guid_character_valid
            Return
        ${EndIf}
        StrCmp $5 '0' legacy_guid_character_valid
        StrCmp $5 '1' legacy_guid_character_valid
        StrCmp $5 '2' legacy_guid_character_valid
        StrCmp $5 '3' legacy_guid_character_valid
        StrCmp $5 '4' legacy_guid_character_valid
        StrCmp $5 '5' legacy_guid_character_valid
        StrCmp $5 '6' legacy_guid_character_valid
        StrCmp $5 '7' legacy_guid_character_valid
        StrCmp $5 '8' legacy_guid_character_valid
        StrCmp $5 '9' legacy_guid_character_valid
        StrCmp $5 'a' legacy_guid_character_valid
        StrCmp $5 'b' legacy_guid_character_valid
        StrCmp $5 'c' legacy_guid_character_valid
        StrCmp $5 'd' legacy_guid_character_valid
        StrCmp $5 'e' legacy_guid_character_valid
        StrCmp $5 'f' legacy_guid_character_valid
        Return
        legacy_guid_character_valid:
    ${Next}
    StrCpy $3 "1"
FunctionEnd

Function CheckLegacyLowerHex64
    StrCpy $3 "0"
    StrLen $4 $2
    ${If} $4 != 64
        Return
    ${EndIf}
    ${ForEach} $4 0 63 + 1
        StrCpy $5 $2 1 $4
        StrCmp $5 '0' legacy_hex_character_valid
        StrCmp $5 '1' legacy_hex_character_valid
        StrCmp $5 '2' legacy_hex_character_valid
        StrCmp $5 '3' legacy_hex_character_valid
        StrCmp $5 '4' legacy_hex_character_valid
        StrCmp $5 '5' legacy_hex_character_valid
        StrCmp $5 '6' legacy_hex_character_valid
        StrCmp $5 '7' legacy_hex_character_valid
        StrCmp $5 '8' legacy_hex_character_valid
        StrCmp $5 '9' legacy_hex_character_valid
        StrCmp $5 'a' legacy_hex_character_valid
        StrCmp $5 'b' legacy_hex_character_valid
        StrCmp $5 'c' legacy_hex_character_valid
        StrCmp $5 'd' legacy_hex_character_valid
        StrCmp $5 'e' legacy_hex_character_valid
        StrCmp $5 'f' legacy_hex_character_valid
        Return
        legacy_hex_character_valid:
    ${Next}
    StrCpy $3 "1"
FunctionEnd

Function CheckLegacyPositiveDecimal
    StrCpy $3 "0"
    StrLen $4 $2
    ${If} $4 == 0
        Return
    ${EndIf}
    StrCpy $5 $2 1
    StrCmp $5 '1' legacy_decimal_first_valid
    StrCmp $5 '2' legacy_decimal_first_valid
    StrCmp $5 '3' legacy_decimal_first_valid
    StrCmp $5 '4' legacy_decimal_first_valid
    StrCmp $5 '5' legacy_decimal_first_valid
    StrCmp $5 '6' legacy_decimal_first_valid
    StrCmp $5 '7' legacy_decimal_first_valid
    StrCmp $5 '8' legacy_decimal_first_valid
    StrCmp $5 '9' legacy_decimal_first_valid
    Return
    legacy_decimal_first_valid:
    IntOp $4 $4 - 1
    ${ForEach} $5 0 $4 + 1
        StrCpy $6 $2 1 $5
        StrCmp $6 '0' legacy_decimal_character_valid
        StrCmp $6 '1' legacy_decimal_character_valid
        StrCmp $6 '2' legacy_decimal_character_valid
        StrCmp $6 '3' legacy_decimal_character_valid
        StrCmp $6 '4' legacy_decimal_character_valid
        StrCmp $6 '5' legacy_decimal_character_valid
        StrCmp $6 '6' legacy_decimal_character_valid
        StrCmp $6 '7' legacy_decimal_character_valid
        StrCmp $6 '8' legacy_decimal_character_valid
        StrCmp $6 '9' legacy_decimal_character_valid
        Return
        legacy_decimal_character_valid:
    ${Next}
    StrCpy $3 "1"
FunctionEnd

Function CheckLegacyRegistration
    StrCpy $MarkerLegacyRegistrationValid "0"
    ${If} $MarkerLegacyOwnershipValid != "1"
        Return
    ${EndIf}
    ClearErrors
    ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${If} ${Errors}
    ${OrIf} $2 != "${APP_PRODUCT_GUID}"
        Return
    ${EndIf}
    ClearErrors
    ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "InstallLocation"
    ${If} ${Errors}
    ${OrIf} $2 != "$INSTDIR"
        Return
    ${EndIf}
    ClearErrors
    ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
    ${If} ${Errors}
    ${OrIf} $2 != ${APP_INSTALLER_SCHEMA}
        Return
    ${EndIf}
    ClearErrors
    ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "DisplayVersion"
    ${If} ${Errors}
    ${OrIf} $2 != "0.0.10"
        Return
    ${EndIf}
    ClearErrors
    ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "InstallationId"
    ${If} ${Errors}
    ${OrIf} $2 != $MarkerLegacyInstallationId
        Return
    ${EndIf}
    ClearErrors
    ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "UninstallPhase"
    ${If} ${Errors}
    ${OrIf} $2 != "Ready"
        Return
    ${EndIf}
    ClearErrors
    ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"
    ${If} ${Errors}
        Return
    ${EndIf}
    ${If} $2 == 1
        ClearErrors
        ReadRegStr $2 HKCU "${UNINSTALL_KEY}" "PathEntry"
        ${If} ${Errors}
        ${OrIf} $2 != "$INSTDIR"
            Return
        ${EndIf}
    ${ElseIf} $2 != 0
        Return
    ${EndIf}
    StrCpy $MarkerLegacyRegistrationValid "1"
FunctionEnd

!macro DefineMarkerCheck PREFIX
Function ${PREFIX}CheckOwnershipMarker
    StrCpy $OwnershipMarkerValid "0"
    StrCpy $MarkerLineCount 0
    StrCpy $MarkerLegacyState 0
    StrCpy $MarkerLegacyInstallationId ""
    StrCpy $MarkerLegacyOwnershipValid "0"
    !insertmacro GetRegularFileState "$INSTDIR\${APP_INSTALLER_MARKER}"
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        Return
    ${EndIf}
    ClearErrors
    FileOpen $0 "$INSTDIR\${APP_INSTALLER_MARKER}" r
    ${If} ${Errors}
        Return
    ${EndIf}
    marker_next:
        ClearErrors
        FileRead $0 $1
        ${If} ${Errors}
            Goto marker_eof
        ${EndIf}
        IntOp $MarkerLineCount $MarkerLineCount + 1
        ${If} $MarkerLineCount > 40
            FileClose $0
            Return
        ${EndIf}
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\r$\n' marker_current
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\n' marker_current
        StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}' marker_current
        ; Exact v0.0.10 Windows PowerShell 5.1 marker grammar. Every line and the
        ; released file order are required, while IDs, hashes, sizes, and the fixed
        ; install location remain bounded value slots.
        ${Select} $MarkerLegacyState
            ${Case} 0
                StrCmp $1 '{$\r$\n' 0 marker_invalid
            ${Case} 1
                StrCmp $1 '    "schemaVersion":  1,$\r$\n' 0 marker_invalid
            ${Case} 2
                StrCmp $1 '    "productGuid":  "${APP_PRODUCT_GUID}",$\r$\n' 0 marker_invalid
            ${Case} 3
                StrCpy $2 $1 24
                StrCmp $2 '    "installationId":  "' 0 marker_invalid
                StrCpy $2 $1 36 24
                Call CheckLegacyCanonicalGUID
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $MarkerLegacyInstallationId $2
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 4
                StrCpy $2 $1 25
                StrCmp $2 '    "installLocation":  "' 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 29
                ${If} $3 <= 0
                    Goto marker_invalid
                ${EndIf}
                StrCpy $2 $1 $3 25
                ${StrRep} $2 $2 "\\" "\"
                StrCmp $2 "$INSTDIR" 0 marker_invalid
            ${Case} 5
                StrCmp $1 '    "installedVersion":  "0.0.10",$\r$\n' 0 marker_invalid
            ${Case} 6
                StrCmp $1 '    "ownedFiles":  [$\r$\n' 0 marker_invalid
            ${Case} 7
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 8
                StrCmp $1 '                           "name":  "${APP_BASE_SCRIPT}",$\r$\n' 0 marker_invalid
            ${Case} 9
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 10
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 11
                StrCmp $1 '                       },$\r$\n' 0 marker_invalid
            ${Case} 12
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 13
                StrCmp $1 '                           "name":  "${APP_LICENSE}",$\r$\n' 0 marker_invalid
            ${Case} 14
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 15
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 16
                StrCmp $1 '                       },$\r$\n' 0 marker_invalid
            ${Case} 17
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 18
                StrCmp $1 '                           "name":  "${APP_STACK_SCRIPT}",$\r$\n' 0 marker_invalid
            ${Case} 19
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 20
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 21
                StrCmp $1 '                       },$\r$\n' 0 marker_invalid
            ${Case} 22
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 23
                StrCmp $1 '                           "name":  "${APP_QUIET_UNINSTALL_HELPER}",$\r$\n' 0 marker_invalid
            ${Case} 24
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 25
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 26
                StrCmp $1 '                       },$\r$\n' 0 marker_invalid
            ${Case} 27
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 28
                StrCmp $1 '                           "name":  "uninstall.exe",$\r$\n' 0 marker_invalid
            ${Case} 29
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 30
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 31
                StrCmp $1 '                       },$\r$\n' 0 marker_invalid
            ${Case} 32
                StrCmp $1 '                       {$\r$\n' 0 marker_invalid
            ${Case} 33
                StrCmp $1 '                           "name":  "${APP_REPLACED_EXECUTABLE}",$\r$\n' 0 marker_invalid
            ${Case} 34
                StrCpy $2 $1 39
                StrCmp $2 '                           "sha256":  "' 0 marker_invalid
                StrCpy $2 $1 64 39
                Call CheckLegacyLowerHex64
                StrCmp $3 "1" 0 marker_invalid
                StrCpy $2 $1 4 -4
                StrCmp $2 '",$\r$\n' 0 marker_invalid
            ${Case} 35
                StrCpy $2 $1 36
                StrCmp $2 '                           "size":  ' 0 marker_invalid
                StrLen $3 $1
                IntOp $3 $3 - 38
                StrCpy $2 $1 $3 36
                Call CheckLegacyPositiveDecimal
                StrCmp $3 "1" 0 marker_invalid
            ${Case} 36
                StrCmp $1 '                       }$\r$\n' 0 marker_invalid
            ${Case} 37
                StrCmp $1 '                   ]$\r$\n' 0 marker_invalid
            ${Case} 38
                StrCmp $1 '}$\r$\n' 0 marker_invalid
            ${Default}
                Goto marker_invalid
        ${EndSelect}
        IntOp $MarkerLegacyState $MarkerLegacyState + 1
        Goto marker_next
    marker_current:
        ; A current marker must be the complete one-line document.
        ${If} $MarkerLineCount != 1
            Goto marker_invalid
        ${EndIf}
        ClearErrors
        FileRead $0 $2
        ${IfNot} ${Errors}
            FileClose $0
            Return
        ${EndIf}
        FileClose $0
        StrCpy $OwnershipMarkerValid "1"
        Return
    marker_eof:
        FileClose $0
        ${If} $MarkerLegacyState == 39
            StrCpy $OwnershipMarkerValid "1"
            StrCpy $MarkerLegacyOwnershipValid "1"
        ${EndIf}
        Return
    marker_invalid:
        FileClose $0
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
!insertmacro DefineMarkerCheck ""

!insertmacro DefinePathHelper ""
!insertmacro DefinePathHelper "un."

Function un.CheckDirectoryResidual
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
        ${IfNot} ${Errors}
            Goto residual_next
        ${EndIf}
    residual_done:
        FindClose $0
FunctionEnd

!macro NotifyPathChanged
    System::Call 'USER32::SendMessageTimeoutW(p ${HWND_BROADCAST}, i ${WM_SETTINGCHANGE}, p 0, w "Environment", i 0x2, i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}, *p .r3) p.r2'
    ${If} $2 == 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "The PATH changed, but Windows did not confirm the refresh. Open a new terminal; sign out and back in if necessary." /SD IDOK
    ${EndIf}
!macroend

!macro VerifyExactOwnershipMarker PATH
    StrCpy $MarkerOperationResult "1"
    !insertmacro GetRegularFileState "${PATH}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        FileOpen $0 "${PATH}" r
        ${IfNot} ${Errors}
            ClearErrors
            FileRead $0 $1
            ${IfNot} ${Errors}
                StrCpy $2 "0"
                StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\r$\n' 0 +2
                    StrCpy $2 "1"
                StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\n' 0 +2
                    StrCpy $2 "1"
                StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}' 0 +2
                    StrCpy $2 "1"
                ${If} $2 == "1"
                    ClearErrors
                    FileRead $0 $1
                    ${If} ${Errors}
                        StrCpy $MarkerOperationResult "0"
                    ${EndIf}
                ${EndIf}
            ${EndIf}
            FileClose $0
        ${EndIf}
    ${EndIf}
!macroend

!macro WriteOwnershipMarker PATH
    StrCpy $MarkerOperationResult "1"
    ; Never follow a directory or reparse point while creating an ownership token.
    !insertmacro GetRegularFileState "${PATH}"
    ${If} $FileState != ${APP_FILE_STATE_UNSAFE}
        ClearErrors
        FileOpen $0 "${PATH}" w
        ${IfNot} ${Errors}
            ClearErrors
            FileWrite $0 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\r$\n'
            FileClose $0
            ${IfNot} ${Errors}
                !insertmacro VerifyExactOwnershipMarker "${PATH}"
            ${EndIf}
        ${EndIf}
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

!macro DeleteRetryable NAME
    !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove ${NAME}."
            Goto uninstall_retryable_failure
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_ABSENT}
                StrCpy $InstallFailureMessage "Could not verify removal of ${NAME}."
                Goto uninstall_retryable_failure
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        StrCpy $InstallFailureMessage "Installer-owned path ${NAME} is inaccessible or not a regular file."
        Goto uninstall_retryable_failure
    ${EndIf}
!macroend

!macro DeleteFinal NAME
    !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $UninstallResidual "1"
            DetailPrint "Could not remove final residual ${NAME}."
            ClearErrors
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_ABSENT}
                StrCpy $UninstallResidual "1"
                DetailPrint "Could not verify removal of final residual ${NAME}."
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        StrCpy $UninstallResidual "1"
        DetailPrint "Final residual ${NAME} is inaccessible or not a regular file."
    ${EndIf}
!macroend

!macro DeleteRequiredAfterRegistration NAME
    !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove ${NAME} after registration deletion."
            Call un.RestoreRetryRegistration
            Goto uninstall_retryable_failure
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${NAME}"
            ${If} $FileState != ${APP_FILE_STATE_ABSENT}
                StrCpy $InstallFailureMessage "Could not verify removal of ${NAME} after registration deletion."
                Call un.RestoreRetryRegistration
                Goto uninstall_retryable_failure
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        StrCpy $InstallFailureMessage "Installer-owned path ${NAME} is inaccessible or not a regular file after registration deletion."
        Call un.RestoreRetryRegistration
        Goto uninstall_retryable_failure
    ${EndIf}
!macroend

Function un.RestoreRetryRegistration
    StrCpy $RegistryRestoreFailed "0"
    !insertmacro VerifyExactOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
    ${If} $MarkerOperationResult != "0"
        !insertmacro WriteOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
        ${If} $MarkerOperationResult != "0"
            StrCpy $RegistryRestoreFailed "1"
            Return
        ${EndIf}
    ${EndIf}
    !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
    ${If} $FileState == ${APP_FILE_STATE_ABSENT}
        !insertmacro GetRegularFileState "$EXEPATH"
        ${If} $FileState == ${APP_FILE_STATE_REGULAR}
            System::Call 'KERNEL32::CopyFileW(w "$EXEPATH", w "$INSTDIR\uninstall.exe", i 0) i.r0'
            ${If} $0 == 0
                StrCpy $RegistryRestoreFailed "1"
                Return
            ${EndIf}
            !insertmacro GetRegularFileState "$INSTDIR\uninstall.exe"
        ${EndIf}
    ${EndIf}
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        StrCpy $RegistryRestoreFailed "1"
        Return
    ${EndIf}
    !insertmacro GetRegularFileState "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}"
    ${If} $FileState != ${APP_FILE_STATE_REGULAR}
        StrCpy $RegistryRestoreFailed "1"
        Return
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\uninstall.exe",0'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$INSTDIR\${APP_QUIET_UNINSTALL_HELPER}" -Uninstaller "$INSTDIR\uninstall.exe" -InstallDirectory "$INSTDIR"'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion" ${APP_INSTALLER_SCHEMA}
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" 0
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
    ${If} ${Errors}
        StrCpy $RegistryRestoreFailed "1"
        Return
    ${EndIf}
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    ${If} ${Errors}
        StrCpy $RegistryRestoreFailed "1"
    ${EndIf}
FunctionEnd

Section "Install"
    !insertmacro AcquireInstallerMutex ${APP_EXIT_INSTALL_FAILED}
    !insertmacro AcquireLifecycleMutex ${APP_EXIT_INSTALL_FAILED}

    StrCpy $ExistingInstallation "0"
    StrCpy $ExistingRegistryOwned "0"
    StrCpy $InstallCompleteWasComplete "0"
    StrCpy $PathOwned "0"
    StrCpy $PathPending "0"
    StrCpy $PathNotificationRequired "0"
    StrCpy $BackupFailed "0"
    StrCpy $PayloadCopyFailed "0"
    StrCpy $RollbackFailed "0"

    Call CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The fixed install path is not a regular directory." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}
    ${DirState} "$INSTDIR" $InstallDirectoryNonEmpty
    ${If} $InstallDirectoryExists == "1"
    ${AndIf} $InstallDirectoryNonEmpty == "-1"
        MessageBox MB_ICONSTOP|MB_OK "The existing install directory could not be enumerated safely. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}
    Call CheckOwnershipMarker
    Call CheckLegacyRegistration
    ${If} $MarkerLegacyOwnershipValid == "1"
    ${AndIf} $MarkerLegacyRegistrationValid != "1"
        MessageBox MB_ICONSTOP|MB_OK "The v0.0.10 marker and registration do not match exactly. Their contents were preserved." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}

    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${IfNot} ${Errors}
        ${If} $0 != "${APP_PRODUCT_GUID}"
            MessageBox MB_ICONSTOP|MB_OK "The fixed product key belongs to another product." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ${If} $OwnershipMarkerValid != "1"
            MessageBox MB_ICONSTOP|MB_OK "The registered directory has no matching ownership marker." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ClearErrors
        ReadRegStr $1 HKCU "${UNINSTALL_KEY}" "InstallLocation"
        ${IfNot} ${Errors}
        ${AndIf} $1 != "$INSTDIR"
            MessageBox MB_ICONSTOP|MB_OK "The registered install location does not match the fixed location." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ClearErrors
        ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
        ${IfNot} ${Errors}
        ${AndIf} $0 != ${APP_INSTALLER_SCHEMA}
            MessageBox MB_ICONSTOP|MB_OK "The existing installer schema is not compatible with this setup." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        StrCpy $ExistingInstallation "1"
        StrCpy $ExistingRegistryOwned "1"
        ${If} $MarkerLegacyRegistrationValid == "1"
            ; v0.0.10 registration is owned but has no current InstallComplete.
            ; The exact marker and registration pair authorizes one replacement.
            StrCpy $InstallCompleteWasComplete "1"
        ${EndIf}
    ${Else}
        ClearErrors
        EnumRegValue $0 HKCU "${UNINSTALL_KEY}" 0
        ${If} ${Errors}
            ClearErrors
            EnumRegKey $0 HKCU "${UNINSTALL_KEY}" 0
        ${EndIf}
        ${IfNot} ${Errors}
            ${If} $OwnershipMarkerValid != "1"
                MessageBox MB_ICONSTOP|MB_OK "The fixed product key contains unowned state." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
            ClearErrors
            ReadRegStr $1 HKCU "${UNINSTALL_KEY}" "InstallLocation"
            ${IfNot} ${Errors}
            ${AndIf} $1 != "$INSTDIR"
                MessageBox MB_ICONSTOP|MB_OK "The incomplete registration points to another location." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
            ClearErrors
            ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
            ${IfNot} ${Errors}
            ${AndIf} $0 != ${APP_INSTALLER_SCHEMA}
                MessageBox MB_ICONSTOP|MB_OK "The incomplete installer schema is not compatible with this setup." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
            StrCpy $ExistingInstallation "1"
            StrCpy $ExistingRegistryOwned "1"
        ${ElseIf} $OwnershipMarkerValid == "1"
            StrCpy $ExistingInstallation "1"
        ${ElseIf} $InstallDirectoryNonEmpty == "1"
            MessageBox MB_ICONSTOP|MB_OK "The fixed install directory is nonempty but unmarked. Its contents were preserved." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
    ${EndIf}

    ${If} $ExistingRegistryOwned == "1"
        ${If} $MarkerLegacyRegistrationValid != "1"
            ClearErrors
            ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
            ${IfNot} ${Errors}
            ${AndIf} $0 == "1"
                StrCpy $InstallCompleteWasComplete "1"
            ${EndIf}
        ${EndIf}
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
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the complete package." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the uninstaller." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}
    !insertmacro WriteOwnershipMarker "$PLUGINSDIR\package\${APP_INSTALLER_MARKER}"
    ${If} $MarkerOperationResult != "0"
        MessageBox MB_ICONSTOP|MB_OK "Could not create the ownership marker." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}

    ; Installed-state mutation starts with creation of the fixed root. Keep
    ; interactive cancellation disabled through commit or complete rollback.
    StrCpy $InstallMutationActive "1"
    Call DisableInstallCancellation
    ClearErrors
    CreateDirectory "$PLUGINSDIR\backup"
    CreateDirectory "$INSTDIR"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the install directory." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}
    !insertmacro BackupFile "${APP_BASE_SCRIPT}"
    !insertmacro BackupFile "${APP_LICENSE}"
    !insertmacro BackupFile "${APP_STACK_SCRIPT}"
    !insertmacro BackupFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro BackupFile "uninstall.exe"
    !insertmacro BackupFile "${APP_EXECUTABLE}"
!ifdef APP_REPLACED_EXECUTABLE
    !insertmacro BackupFile "${APP_REPLACED_EXECUTABLE}"
!endif
    !insertmacro BackupFile "${APP_INSTALLER_MARKER}"
    ${If} $BackupFailed == "1"
        MessageBox MB_ICONSTOP|MB_OK "Could not back up the existing installer-owned files." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}

    ${If} $ExistingRegistryOwned == "1"
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Could not mark the existing installation incomplete." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
    ${EndIf}

    ; The marker is first so an interrupted copy is repairable. The executable is last.
    !insertmacro InstallFile "${APP_INSTALLER_MARKER}"
    !insertmacro InstallFile "${APP_BASE_SCRIPT}"
    !insertmacro InstallFile "${APP_LICENSE}"
    !insertmacro InstallFile "${APP_STACK_SCRIPT}"
    !insertmacro InstallFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro InstallFile "uninstall.exe"
    !insertmacro InstallFile "${APP_EXECUTABLE}"
!ifdef APP_REPLACED_EXECUTABLE
    !insertmacro GetRegularFileState "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
    ${If} $FileState == ${APP_FILE_STATE_REGULAR}
        ClearErrors
        Delete "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not remove replaced executable ${APP_REPLACED_EXECUTABLE}."
        ${Else}
            !insertmacro GetRegularFileState "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
            ${If} $FileState != ${APP_FILE_STATE_ABSENT}
                StrCpy $PayloadCopyFailed "1"
                StrCpy $InstallFailureMessage "Could not verify removal of replaced executable ${APP_REPLACED_EXECUTABLE}."
            ${EndIf}
        ${EndIf}
    ${ElseIf} $FileState != ${APP_FILE_STATE_ABSENT}
        StrCpy $PayloadCopyFailed "1"
        StrCpy $InstallFailureMessage "Replaced executable ${APP_REPLACED_EXECUTABLE} is inaccessible or not a regular file."
    ${EndIf}
!endif
    ${If} $PayloadCopyFailed == "1"
        Goto install_rollback
    ${EndIf}
    !insertmacro VerifyExactOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
    ${If} $MarkerOperationResult != "0"
        StrCpy $PayloadCopyFailed "1"
        StrCpy $InstallFailureMessage "The installed ownership marker did not validate."
        Goto install_rollback
    ${EndIf}

    StrCpy $PathAction "Contains"
    Call RunPathHelper
    ${If} $0 != "0"
        StrCpy $InstallFailureMessage "PATH inspection failed: status $0. $1"
        Goto install_integration_failure
    ${EndIf}
    ${If} $1 == "1"
        ; A pending intent plus a present entry is ambiguous after a crash: another
        ; process may have added the same entry. Clear the intent without claiming
        ; ownership. This may leave a stale installer-created entry in the narrow
        ; crash window, but it never removes an entry that could belong to others.
        ${If} $PathPending == "1"
            StrCpy $PathOwned "0"
            StrCpy $PathNotificationRequired "1"
            ClearErrors
            DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
            ${If} ${Errors}
                StrCpy $InstallFailureMessage "Ambiguous PATH ownership intent could not be cleared."
                Goto install_integration_failure
            ${EndIf}
            StrCpy $PathPending "0"
        ${EndIf}
    ${ElseIf} $1 == "0"
        ; An absent entry invalidates previously recorded literal ownership.
        ; Ownership is restored only when this helper invocation changes PATH.
        StrCpy $PathOwned "0"
        ${If} $PathPending != "1"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAddPending" 1
            ${If} ${Errors}
                StrCpy $InstallFailureMessage "PATH ownership intent could not be recorded."
                Goto install_integration_failure
            ${EndIf}
            StrCpy $PathPending "1"
        ${EndIf}
        StrCpy $PathAction "Add"
        Call RunPathHelper
        ${If} $0 == "10"
            StrCpy $PathOwned "1"
            StrCpy $PathNotificationRequired "1"
        ${ElseIf} $0 == "0"
            ; A concurrent writer added it. Verify, but do not claim new ownership.
            StrCpy $PathAction "Contains"
            Call RunPathHelper
            ${If} $0 != "0"
            ${OrIf} $1 != "1"
                StrCpy $InstallFailureMessage "PATH verification failed after Add."
                Goto install_integration_failure
            ${EndIf}
            ; This Add made no change, so its pending intent must not become ownership on retry.
            ${If} $PathPending == "1"
                ClearErrors
                DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
                ${If} ${Errors}
                    StrCpy $InstallFailureMessage "No-change PATH ownership intent could not be cleared."
                    Goto install_integration_failure
                ${EndIf}
                StrCpy $PathPending "0"
            ${EndIf}
        ${Else}
            StrCpy $InstallFailureMessage "PATH Add failed: status $0. $1"
            Goto install_integration_failure
        ${EndIf}
    ${Else}
        StrCpy $InstallFailureMessage "The PATH helper returned an invalid Contains result."
        Goto install_integration_failure
    ${EndIf}
    ${If} $PathNotificationRequired == "1"
        !insertmacro NotifyPathChanged
    ${EndIf}

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
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion" ${APP_INSTALLER_SCHEMA}
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $PathOwned
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Windows Installed Apps registration failed."
        Goto install_integration_failure
    ${EndIf}
    ${If} $PathPending == "1"
        ClearErrors
        DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "PATH ownership intent could not be cleared."
            Goto install_integration_failure
        ${EndIf}
        StrCpy $PathPending "0"
    ${EndIf}
    ; ProductGuid is written only after the rest of the registration succeeds, so
    ; an interrupted fresh install remains recognizable as repairable incomplete state.
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "ProductGuid" "${APP_PRODUCT_GUID}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "ProductGuid could not be committed."
        Goto install_integration_failure
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "InstallComplete could not be committed."
        Goto install_integration_failure
    ${EndIf}
    ; DisplayVersion is the WinGet-visible commit marker and is always written last.
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    ${If} ${Errors}
        ; DisplayVersion is the final visible commit. If it cannot be written,
        ; force the registration back to an explicitly incomplete repair state.
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 0
        StrCpy $InstallFailureMessage "DisplayVersion could not be committed."
        Goto install_integration_failure
    ${EndIf}

    DetailPrint "Creating default configuration when missing..."
    ; Setup remains serialized, but the application command may acquire its lifecycle gate.
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

    install_integration_failure:
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
!ifdef APP_REPLACED_EXECUTABLE
        !insertmacro RestoreFile "${APP_REPLACED_EXECUTABLE}"
!endif
        ; Restore the previous marker exactly when it was backed up. For a fresh
        ; install, remove the new marker only after every other rollback step
        ; succeeded. If rollback is already incomplete, preserve a current marker
        ; so the next setup run can identify and repair the directory.
        !insertmacro GetRegularFileState "$PLUGINSDIR\backup\${APP_INSTALLER_MARKER}"
        ${If} $FileState == ${APP_FILE_STATE_REGULAR}
            !insertmacro RestoreFile "${APP_INSTALLER_MARKER}"
        ${ElseIf} $FileState == ${APP_FILE_STATE_ABSENT}
            ${If} $RollbackFailed == "0"
                !insertmacro RestoreFile "${APP_INSTALLER_MARKER}"
            ${EndIf}
        ${Else}
            ; The private backup path must be either a regular file or absent.
            StrCpy $RollbackFailed "1"
        ${EndIf}

        ; Never trust the marker state captured before replacement. A failed copy
        ; can leave the destination regular but truncated. Existing installations
        ; must finish rollback with a valid ownership marker; incomplete fresh
        ; rollbacks must retain a current exact marker for repair.
        ${If} $ExistingInstallation == "1"
            Call CheckOwnershipMarker
            ${If} $OwnershipMarkerValid != "1"
                !insertmacro WriteOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
                ${If} $MarkerOperationResult == "0"
                    Call CheckOwnershipMarker
                ${EndIf}
                ${If} $OwnershipMarkerValid != "1"
                    StrCpy $RollbackFailed "1"
                ${EndIf}
            ${EndIf}
        ${ElseIf} $RollbackFailed != "0"
            !insertmacro VerifyExactOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
            ${If} $MarkerOperationResult != "0"
                !insertmacro WriteOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
                ${If} $MarkerOperationResult != "0"
                    StrCpy $RollbackFailed "1"
                ${EndIf}
            ${EndIf}
        ${EndIf}

        ${If} $RollbackFailed == "0"
        ${AndIf} $ExistingRegistryOwned == "1"
        ${AndIf} $InstallCompleteWasComplete == "1"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallComplete" 1
            ${If} ${Errors}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}
        ; A prior installation must keep its restored directory. Only a fresh
        ; installation rollback should remove the now-empty install root.
        ${If} $ExistingInstallation == "0"
            ClearErrors
            RMDir "$INSTDIR"
            ${If} ${Errors}
                ; Any fresh directory that survives rollback must remain
                ; identifiable even if another file appeared late.
                Call CheckOwnershipMarker
                ${If} $OwnershipMarkerValid != "1"
                    !insertmacro WriteOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
                    ${If} $MarkerOperationResult == "0"
                        Call CheckOwnershipMarker
                    ${EndIf}
                ${EndIf}
                StrCpy $RollbackFailed "1"
            ${EndIf}
        ${EndIf}
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The previous files were restored." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Rollback was incomplete; run setup again." /SD IDOK
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
    StrCpy $UninstallResidual "0"

    Call un.CheckInstallDirectory
    ${If} $InstallDirectorySafe != "1"
        MessageBox MB_ICONSTOP|MB_OK "The fixed install path is not a regular directory." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "ProductGuid"
    ${If} ${Errors}
    ${OrIf} $0 != "${APP_PRODUCT_GUID}"
        MessageBox MB_ICONSTOP|MB_OK "The product registration does not match this uninstaller." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
    ${If} ${Errors}
    ${OrIf} $0 != "$INSTDIR"
        MessageBox MB_ICONSTOP|MB_OK "The registered install location does not match this uninstaller." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
    ${If} ${Errors}
    ${OrIf} $0 != "1"
        MessageBox MB_ICONSTOP|MB_OK "Run setup once to repair the incomplete installation before uninstalling." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
    ${If} ${Errors}
    ${OrIf} $0 != ${APP_INSTALLER_SCHEMA}
        MessageBox MB_ICONSTOP|MB_OK "The installed schema is missing or not compatible with this uninstaller. Run the matching setup to repair or remove it." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit
    ${EndIf}
    !insertmacro VerifyExactOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
    ${If} $MarkerOperationResult != "0"
        MessageBox MB_ICONSTOP|MB_OK "The current ownership marker is missing or does not match this uninstaller." /SD IDOK
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
    ; present, that attempt did not cross the safe deletion boundary. Inaccessible
    ; or non-regular owned paths require setup repair instead of being treated as absent.
    !insertmacro GetRegularFileState "$INSTDIR\${APP_EXECUTABLE}"
    StrCpy $9 $FileState
!ifdef APP_REPLACED_EXECUTABLE
    !insertmacro GetRegularFileState "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
    StrCpy $8 $FileState
!else
    StrCpy $8 ${APP_FILE_STATE_ABSENT}
!endif
    ${If} $9 == ${APP_FILE_STATE_UNSAFE}
    ${OrIf} $8 == ${APP_FILE_STATE_UNSAFE}
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
        ${ElseIf} $8 == ${APP_FILE_STATE_REGULAR}
            MessageBox MB_ICONSTOP|MB_OK "A replaced executable remains but the current executable is missing. Run setup once to repair the installation, then uninstall again." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
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
    ; terminal cleanup, PATH, file, registry, marker, and directory decision.
    StrCpy $InstallMutationActive "1"
    Call un.DisableUninstallCancellation
    ${If} $CleanupComplete != "1"
        ; Keep the lifecycle gate continuously owned so no normal application
        ; command can recreate state between cleanup and executable removal. The
        ; hidden cleanup command must honor --installer-lifecycle-lock-held and
        ; skip acquiring this same mutex itself.
        SetOutPath "$INSTDIR"
        ${If} $DeleteConfigurationOnUninstall == "1"
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=${APP_INSTALLER_SCHEMA} --installer-lifecycle-lock-held --delete-configuration'
        ${Else}
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=${APP_INSTALLER_SCHEMA} --installer-lifecycle-lock-held'
        ${EndIf}
        Pop $0
        Pop $1
        SetOutPath "$TEMP"
        ${If} $0 != "0"
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup failed with status $0. $1 No installer-owned files were removed." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            !insertmacro ReleaseAllMutexes
            Quit
        ${EndIf}
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup succeeded but could not be recorded. Retrying is safe." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            Quit
        ${EndIf}
        ; Until every runnable product executable is gone, a retry must rerun
        ; cleanup because the application can recreate state between attempts.
        StrCpy $CleanupRetryRequired "1"
    ${EndIf}

    ${If} $PathOwned == "1"
        StrCpy $PathAction "Remove"
        Call un.RunPathHelper
        ${If} $0 != "0"
        ${AndIf} $0 != "10"
            StrCpy $InstallFailureMessage "PATH removal failed with status $0. $1"
            Goto uninstall_retryable_failure
        ${EndIf}
        ; A zero result can be recovery after a prior removal completed before notification.
        !insertmacro NotifyPathChanged
    ${EndIf}

    ; Remove every runnable product executable before deleting support files.
    ; If either executable cannot be removed, cleanup is reset for a safe retry.
!ifdef APP_REPLACED_EXECUTABLE
    !insertmacro DeleteRetryable "${APP_REPLACED_EXECUTABLE}"
!endif
    !insertmacro DeleteRetryable "${APP_EXECUTABLE}"
    StrCpy $CleanupRetryRequired "0"

    ; Registration and uninstall entry points remain until ordinary support files are gone.
    !insertmacro DeleteRetryable "${APP_BASE_SCRIPT}"
    !insertmacro DeleteRetryable "${APP_LICENSE}"
    !insertmacro DeleteRetryable "${APP_STACK_SCRIPT}"

    ClearErrors
    DeleteRegKey HKCU "${UNINSTALL_KEY}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Could not fully remove the Windows Installed Apps registration."
        Call un.RestoreRetryRegistration
        Goto uninstall_retryable_failure
    ${EndIf}
    !insertmacro DeleteRequiredAfterRegistration "uninstall.exe"
    !insertmacro DeleteFinal "${APP_QUIET_UNINSTALL_HELPER}"

    Call un.CheckDirectoryResidual
    ${If} $InstallDirectoryHasUnknownEntries == "error"
        StrCpy $UninstallResidual "1"
        DetailPrint "Could not inspect the remaining install directory; the marker was preserved."
    ${ElseIf} $InstallDirectoryHasUnknownEntries == "1"
        DetailPrint "Unknown files remain; the marker and directory were preserved."
    ${Else}
        !insertmacro DeleteFinal "${APP_INSTALLER_MARKER}"
        !insertmacro GetRegularFileState "$INSTDIR\${APP_INSTALLER_MARKER}"
        ${If} $FileState == ${APP_FILE_STATE_ABSENT}
            ClearErrors
            RMDir "$INSTDIR"
            ${If} ${Errors}
                ; A file appeared after enumeration. Restore and validate the marker
                ; so setup can safely repair the remaining directory.
                !insertmacro WriteOwnershipMarker "$INSTDIR\${APP_INSTALLER_MARKER}"
                ${If} $MarkerOperationResult != "0"
                    MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} was removed, but the residual directory could not be marked for safe repair." /SD IDOK
                    SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
                    !insertmacro ReleaseAllMutexes
                    Goto uninstall_done
                ${EndIf}
                StrCpy $UninstallResidual "1"
                DetailPrint "Could not remove the final install directory."
                ClearErrors
            ${EndIf}
        ${Else}
            ; DeleteFinal already recorded the residual. Never treat an inaccessible
            ; marker as absent and never remove the directory without a verified state.
            StrCpy $UninstallResidual "1"
        ${EndIf}
    ${EndIf}
    ${If} $UninstallResidual == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} was removed, but an inert installer residual could not be deleted." /SD IDOK
    ${EndIf}
    Goto uninstall_done

    uninstall_retryable_failure:
        ${If} $RegistryRestoreFailed == "1"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Retry registration could not be fully restored. Preserve the remaining uninstaller and marker, then run setup to repair the installation." /SD IDOK
        ${ElseIf} $CleanupRetryRequired == "1"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
            ${If} ${Errors}
                MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Cleanup succeeded, but its retry state could not be reset. Run setup once to repair the installation, then uninstall again." /SD IDOK
            ${Else}
                MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The uninstaller and ownership marker were preserved. Cleanup will run again on the next uninstall attempt." /SD IDOK
            ${EndIf}
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The uninstaller and ownership marker were preserved; run setup or uninstall again." /SD IDOK
        ${EndIf}
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        !insertmacro ReleaseAllMutexes
        Quit

    uninstall_done:
        StrCpy $InstallMutationActive "0"
        !insertmacro ReleaseAllMutexes
SectionEnd
