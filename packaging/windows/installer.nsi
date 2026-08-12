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
!include "WordFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "WinVer.nsh"
!include "x64.nsh"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_KEY}"
!define APP_INSTALLER_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerExclusive"
!define APP_LIFECYCLE_MUTEX_NAME "Local\${APP_APPLICATION_NAME}-lifecycle-v1"
!define APP_INSTALLER_SCHEMA 1
!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 100
!define APP_ERROR_FILE_NOT_FOUND 2
!define APP_ERROR_PATH_NOT_FOUND 3
!define APP_WAIT_OBJECT_0 0
!define APP_WAIT_ABANDONED 128
!define APP_WAIT_TIMEOUT 258
!define APP_FILE_ATTRIBUTE_DIRECTORY 0x10
!define APP_FILE_ATTRIBUTE_REPARSE_POINT 0x400
!define APP_EXIT_INSTALLER_BUSY 41
!define APP_EXIT_UNSUPPORTED_PLATFORM 50
!define APP_EXIT_INSTALL_FAILED 70
!define APP_EXIT_UNINSTALL_FAILED 80

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var InstallerMutexHandle
Var LifecycleMutexHandle
Var ExistingInstallation
Var ExistingRegistryOwned
Var InstallCompleteWasComplete
Var OwnershipMarkerValid
Var LegacyOwnershipMarkerValid
Var MarkerWritePath
Var InstallDirectorySafe
Var InstallDirectoryNonEmpty
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
        SetErrorLevel ${APP_EXIT_INSTALLER_BUSY}
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
        MessageBox MB_ICONSTOP|MB_OK "Windows could not create the ${APP_DISPLAY_NAME} cleanup gate." /SD IDOK
        SetErrorLevel ${FAILURE_CODE}
        Quit
    ${EndIf}
    System::Call 'KERNEL32::WaitForSingleObject(p $1, i 0) i.r0'
    ${If} $0 == ${APP_WAIT_OBJECT_0}
    ${OrIf} $0 == ${APP_WAIT_ABANDONED}
        Nop
    ${Else}
        System::Call 'KERNEL32::CloseHandle(p $1) i.r0'
        StrCpy $LifecycleMutexHandle 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "A ${APP_DISPLAY_NAME} command is currently changing application state. Wait for it to finish, then uninstall again." /SD IDOK
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
    !insertmacro AcquireInstallerMutex ${FAILURE_CODE}
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
!macroend

Function OpenInstalledConfiguration
    IfSilent done
    ${If} $ExistingInstallation == "1"
        Goto done
    ${EndIf}
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
FunctionEnd

Function .onGUIEnd
    !insertmacro ReleaseInstallerMutex
FunctionEnd

Function .onInstSuccess
    !insertmacro ReleaseInstallerMutex
FunctionEnd

Function .onInstFailed
    !insertmacro ReleaseInstallerMutex
FunctionEnd

Function un.onInit
    !insertmacro InitializeRuntime ${APP_EXIT_UNINSTALL_FAILED}
    StrCpy $DeleteConfigurationOnUninstall "0"
    ${GetParameters} $0
    StrCpy $0 " $0 "
    ClearErrors
    ${GetOptions} $0 " /DELETE_CONFIG " $1
    ${IfNot} ${Errors}
        StrCpy $DeleteConfigurationOnUninstall "1"
    ${EndIf}
FunctionEnd

Function un.onGUIEnd
    !insertmacro ReleaseLifecycleMutex
    !insertmacro ReleaseInstallerMutex
FunctionEnd

Function un.onUninstSuccess
    !insertmacro ReleaseLifecycleMutex
    !insertmacro ReleaseInstallerMutex
FunctionEnd

Function un.onUninstFailed
    !insertmacro ReleaseLifecycleMutex
    !insertmacro ReleaseInstallerMutex
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
        IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
        ${If} $1 == 0
        ${OrIf} $2 != 0
            StrCpy $InstallDirectorySafe "0"
        ${EndIf}
    ${EndIf}
FunctionEnd
!macroend

!macro DefineMarkerFunctions PREFIX
Function ${PREFIX}CheckMarkerAtPath
    StrCpy $OwnershipMarkerValid "0"
    StrCpy $LegacyOwnershipMarkerValid "0"
    System::Call 'KERNEL32::GetFileAttributesW(w "$MarkerWritePath") i.r0'
    ${If} $0 == -1
        Return
    ${EndIf}
    IntOp $1 $0 & ${APP_FILE_ATTRIBUTE_DIRECTORY}
    IntOp $2 $0 & ${APP_FILE_ATTRIBUTE_REPARSE_POINT}
    ${If} $1 != 0
    ${OrIf} $2 != 0
        Return
    ${EndIf}
    ClearErrors
    FileOpen $0 "$MarkerWritePath" r
    ${If} ${Errors}
        Return
    ${EndIf}
    ClearErrors
    FileRead $0 $1
    ${If} ${Errors}
        FileClose $0
        Return
    ${EndIf}
    StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\r$\n' marker_check_eof
    StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\n' marker_check_eof
    StrCmp $1 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}' marker_check_eof
    marker_legacy_next:
        StrCmp $1 '    "productGuid":  "${APP_PRODUCT_GUID}",$\r$\n' marker_legacy_found
        StrCmp $1 '    "productGuid":  "${APP_PRODUCT_GUID}",$\n' marker_legacy_found
        ClearErrors
        FileRead $0 $1
        ${If} ${Errors}
            FileClose $0
            Return
        ${EndIf}
        Goto marker_legacy_next
    marker_legacy_found:
        FileClose $0
        StrCpy $LegacyOwnershipMarkerValid "1"
        Return
    marker_check_eof:
        ClearErrors
        FileRead $0 $2
        ${IfNot} ${Errors}
            FileClose $0
            Return
        ${EndIf}
        Goto marker_found
    FileClose $0
    Return
    marker_found:
        FileClose $0
        StrCpy $OwnershipMarkerValid "1"
FunctionEnd

Function ${PREFIX}CheckOwnershipMarker
    StrCpy $MarkerWritePath "$INSTDIR\${APP_INSTALLER_MARKER}"
    Call ${PREFIX}CheckMarkerAtPath
FunctionEnd

Function ${PREFIX}WriteOwnershipMarker
    StrCpy $OwnershipMarkerValid "0"
    ClearErrors
    FileOpen $0 "$MarkerWritePath" w
    ${If} ${Errors}
        Return
    ${EndIf}
    ClearErrors
    FileWrite $0 '{"productGuid":"${APP_PRODUCT_GUID}","installerSchema":${APP_INSTALLER_SCHEMA}}$\r$\n'
    FileClose $0
    ${If} ${Errors}
        Return
    ${EndIf}
    Call ${PREFIX}CheckMarkerAtPath
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
!insertmacro DefineMarkerFunctions ""
!insertmacro DefineMarkerFunctions "un."
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

!macro BackupFile NAME
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
            ${EndIf}
        ${EndIf}
    ${EndIf}
!macroend

!macro InstallFile NAME
    ${If} $PayloadCopyFailed == "0"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\package\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not install ${NAME}."
        ${EndIf}
    ${EndIf}
!macroend

!macro RestoreFile NAME
    ${If} ${FileExists} "$PLUGINSDIR\backup\${NAME}"
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

!macro DeleteRetryable NAME
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $InstallFailureMessage "Could not remove ${NAME}."
            Goto uninstall_retryable_failure
        ${EndIf}
    ${EndIf}
!macroend

!macro DeleteFinal NAME
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        Delete "$INSTDIR\${NAME}"
        ${If} ${Errors}
            StrCpy $UninstallResidual "1"
            DetailPrint "Could not remove final residual ${NAME}."
            ClearErrors
        ${EndIf}
    ${EndIf}
!macroend

Section "Install"
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
    Call CheckOwnershipMarker

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
        StrCpy $ExistingInstallation "1"
        StrCpy $ExistingRegistryOwned "1"
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
            StrCpy $ExistingInstallation "1"
            StrCpy $ExistingRegistryOwned "1"
        ${ElseIf} $OwnershipMarkerValid == "1"
            StrCpy $ExistingInstallation "1"
        ${ElseIf} $LegacyOwnershipMarkerValid == "1"
            ; One released pre-schema marker is adopted only at the fixed path.
            StrCpy $ExistingInstallation "1"
        ${ElseIf} $InstallDirectoryNonEmpty == "1"
            MessageBox MB_ICONSTOP|MB_OK "The fixed install directory is nonempty but unmarked. Its contents were preserved." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
    ${EndIf}

    ${If} $ExistingRegistryOwned == "1"
        ClearErrors
        ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
        ${IfNot} ${Errors}
        ${AndIf} $0 > ${APP_INSTALLER_SCHEMA}
            MessageBox MB_ICONSTOP|MB_OK "The installed package uses a newer installer schema. Use a current setup." /SD IDOK
            SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
            Quit
        ${EndIf}
        ClearErrors
        ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "DisplayVersion"
        ${IfNot} ${Errors}
            ${VersionCompare} "$0" "${VERSION}" $1
            ${If} $1 == "1"
                MessageBox MB_ICONSTOP|MB_OK "A newer ${APP_DISPLAY_NAME} version is already installed. Downgrade is not supported." /SD IDOK
                SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
                Quit
            ${EndIf}
        ${EndIf}
        ClearErrors
        ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallComplete"
        ${IfNot} ${Errors}
        ${AndIf} $0 == "1"
            StrCpy $InstallCompleteWasComplete "1"
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
    StrCpy $MarkerWritePath "$PLUGINSDIR\package\${APP_INSTALLER_MARKER}"
    Call WriteOwnershipMarker
    ${If} $OwnershipMarkerValid != "1"
        MessageBox MB_ICONSTOP|MB_OK "Could not create the ownership marker." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}

    ClearErrors
    CreateDirectory "$PLUGINSDIR\backup"
    CreateDirectory "$INSTDIR"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the backup directory." /SD IDOK
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

    !insertmacro InstallFile "${APP_INSTALLER_MARKER}"
    !insertmacro InstallFile "${APP_BASE_SCRIPT}"
    !insertmacro InstallFile "${APP_LICENSE}"
    !insertmacro InstallFile "${APP_STACK_SCRIPT}"
    !insertmacro InstallFile "${APP_QUIET_UNINSTALL_HELPER}"
    !insertmacro InstallFile "uninstall.exe"
    !insertmacro InstallFile "${APP_EXECUTABLE}"
!ifdef APP_REPLACED_EXECUTABLE
    ${If} ${FileExists} "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
        ClearErrors
        Delete "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
        ${If} ${Errors}
            StrCpy $PayloadCopyFailed "1"
            StrCpy $InstallFailureMessage "Could not remove replaced executable ${APP_REPLACED_EXECUTABLE}."
        ${EndIf}
    ${EndIf}
!endif
    ${If} $PayloadCopyFailed == "1"
        Goto install_rollback
    ${EndIf}

    StrCpy $PathAction "Contains"
    Call RunPathHelper
    ${If} $0 != "0"
        StrCpy $InstallFailureMessage "PATH inspection failed: status $0. $1"
        Goto install_integration_failure
    ${EndIf}
    ${If} $1 == "1"
        ${If} $PathPending == "1"
            ; Presence after an interrupted intent does not prove who added it.
            StrCpy $PathOwned "0"
            ClearErrors
            DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAddPending"
            ${If} ${Errors}
                StrCpy $InstallFailureMessage "Stale PATH ownership intent could not be cleared."
                Goto install_integration_failure
            ${EndIf}
            StrCpy $PathPending "0"
        ${EndIf}
    ${ElseIf} $1 == "0"
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
            StrCpy $PathAction "Contains"
            Call RunPathHelper
            ${If} $0 != "0"
            ${OrIf} $1 != "1"
                StrCpy $InstallFailureMessage "PATH verification failed after Add."
                Goto install_integration_failure
            ${EndIf}
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
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $PathOwned
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion" ${APP_INSTALLER_SCHEMA}
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "MinimumCompatibleUninstallerSchema" ${APP_INSTALLER_SCHEMA}
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
    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "DisplayVersion could not be committed."
        Goto install_integration_failure
    ${EndIf}

    DetailPrint "Creating default configuration when missing..."
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
        MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The complete payload remains installed. Run setup again to repair integration." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
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
        ${If} ${FileExists} "$PLUGINSDIR\backup\${APP_INSTALLER_MARKER}"
            !insertmacro RestoreFile "${APP_INSTALLER_MARKER}"
        ${ElseIf} $RollbackFailed == "0"
            !insertmacro RestoreFile "${APP_INSTALLER_MARKER}"
        ${Else}
            StrCpy $MarkerWritePath "$INSTDIR\${APP_INSTALLER_MARKER}"
            Call WriteOwnershipMarker
            ${If} $OwnershipMarkerValid != "1"
                StrCpy $RollbackFailed "1"
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
        ClearErrors
        RMDir "$INSTDIR"
        ${If} ${Errors}
        ${AndIf} $OwnershipMarkerValid != "1"
            StrCpy $MarkerWritePath "$INSTDIR\${APP_INSTALLER_MARKER}"
            Call WriteOwnershipMarker
            StrCpy $RollbackFailed "1"
        ${EndIf}
        ${If} $RollbackFailed == "0"
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage The previous files were restored." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "$InstallFailureMessage Rollback was incomplete; run setup again." /SD IDOK
        ${EndIf}
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit

    install_done:
SectionEnd

Section "Uninstall"
    SetAutoClose true
    SetOutPath "$TEMP"
    StrCpy $CleanupComplete "0"
    StrCpy $CleanupRetryRequired "0"
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
    Call un.CheckOwnershipMarker
    ${If} $OwnershipMarkerValid != "1"
        MessageBox MB_ICONSTOP|MB_OK "The ownership marker is missing or does not match." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "InstallerSchemaVersion"
    ${If} ${Errors}
    ${OrIf} $0 != ${APP_INSTALLER_SCHEMA}
        MessageBox MB_ICONSTOP|MB_OK "The installer schema does not match this uninstaller. Run current setup once, then uninstall again." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 HKCU "${UNINSTALL_KEY}" "MinimumCompatibleUninstallerSchema"
    ${If} ${Errors}
    ${OrIf} $0 > ${APP_INSTALLER_SCHEMA}
        MessageBox MB_ICONSTOP|MB_OK "This uninstaller is older than the installed package requires. Use a current setup." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
        Quit
    ${EndIf}
    ClearErrors
    ReadRegDWORD $CleanupComplete HKCU "${UNINSTALL_KEY}" "CleanupComplete"
    ${If} ${Errors}
    ${OrIf} $CleanupComplete != "1"
        StrCpy $CleanupComplete "0"
    ${EndIf}
    ${If} $CleanupComplete == "1"
        ${If} ${FileExists} "$INSTDIR\${APP_EXECUTABLE}"
            ClearErrors
            WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 0
            ${If} ${Errors}
                MessageBox MB_ICONSTOP|MB_OK "The retry cleanup state could not be reset. Run setup once to repair the installation, then uninstall again." /SD IDOK
                SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
                Quit
            ${EndIf}
            StrCpy $CleanupComplete "0"
!ifdef APP_REPLACED_EXECUTABLE
        ${ElseIf} ${FileExists} "$INSTDIR\${APP_REPLACED_EXECUTABLE}"
            MessageBox MB_ICONSTOP|MB_OK "A replaced executable remains but the current executable is missing. Run setup once to repair the installation, then uninstall again." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            Quit
!endif
        ${EndIf}
    ${EndIf}
    ${If} $CleanupComplete == "1"
        StrCpy $DeleteConfigurationOnUninstall "0"
    ${EndIf}
    ClearErrors
    ReadRegDWORD $PathOwned HKCU "${UNINSTALL_KEY}" "PathAdded"
    ${If} ${Errors}
    ${OrIf} $PathOwned != "1"
        StrCpy $PathOwned "0"
    ${EndIf}

    ${If} $CleanupComplete != "1"
        !insertmacro AcquireLifecycleMutex ${APP_EXIT_UNINSTALL_FAILED}
        SetOutPath "$INSTDIR"
        ${If} $DeleteConfigurationOnUninstall == "1"
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1 --installer-lock-held --delete-configuration'
        ${Else}
            nsExec::ExecToStack '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1 --installer-lock-held'
        ${EndIf}
        Pop $0
        Pop $1
        SetOutPath "$TEMP"
        ${If} $0 != "0"
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup failed with status $0. $1 No installer-owned files were removed." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            Quit
        ${EndIf}
        ClearErrors
        WriteRegDWORD HKCU "${UNINSTALL_KEY}" "CleanupComplete" 1
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Application cleanup succeeded but could not be recorded. Retrying is safe." /SD IDOK
            SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
            Quit
        ${EndIf}
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
        !insertmacro NotifyPathChanged
    ${EndIf}

!ifdef APP_REPLACED_EXECUTABLE
    !insertmacro DeleteRetryable "${APP_REPLACED_EXECUTABLE}"
!endif
    !insertmacro DeleteRetryable "${APP_EXECUTABLE}"
    StrCpy $CleanupRetryRequired "0"
    !insertmacro DeleteRetryable "${APP_BASE_SCRIPT}"
    !insertmacro DeleteRetryable "${APP_LICENSE}"
    !insertmacro DeleteRetryable "${APP_STACK_SCRIPT}"

    ClearErrors
    DeleteRegKey HKCU "${UNINSTALL_KEY}"
    ${If} ${Errors}
        StrCpy $InstallFailureMessage "Could not fully remove the Windows Installed Apps registration."
        Goto uninstall_retryable_failure
    ${EndIf}
    !insertmacro DeleteFinal "uninstall.exe"
    !insertmacro DeleteFinal "${APP_QUIET_UNINSTALL_HELPER}"

    Call un.CheckDirectoryResidual
    ${If} $InstallDirectoryHasUnknownEntries == "error"
        StrCpy $UninstallResidual "1"
        DetailPrint "Could not inspect the remaining install directory; the marker was preserved."
    ${ElseIf} $InstallDirectoryHasUnknownEntries == "1"
        DetailPrint "Unknown files remain; the marker and directory were preserved."
    ${Else}
        !insertmacro DeleteFinal "${APP_INSTALLER_MARKER}"
        ${IfNot} ${FileExists} "$INSTDIR\${APP_INSTALLER_MARKER}"
            ClearErrors
            RMDir "$INSTDIR"
            ${If} ${Errors}
                StrCpy $MarkerWritePath "$INSTDIR\${APP_INSTALLER_MARKER}"
                Call un.WriteOwnershipMarker
                ${If} $OwnershipMarkerValid != "1"
                    MessageBox MB_ICONSTOP|MB_OK "The application was removed, but setup ownership could not be restored after final directory cleanup failed." /SD IDOK
                    SetErrorLevel ${APP_EXIT_UNINSTALL_FAILED}
                    Quit
                ${EndIf}
                StrCpy $UninstallResidual "1"
                DetailPrint "Could not remove the final install directory; repair ownership was restored."
            ${EndIf}
        ${EndIf}
    ${EndIf}
    ${If} $UninstallResidual == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} was removed, but an inert installer residual could not be deleted." /SD IDOK
    ${EndIf}
    Goto uninstall_done

    uninstall_retryable_failure:
        ${If} $CleanupRetryRequired == "1"
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
        Quit

    uninstall_done:
SectionEnd
