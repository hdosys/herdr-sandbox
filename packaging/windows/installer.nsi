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
!ifndef OUTPUT_FILE
    !error "OUTPUT_FILE is required"
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
!ifndef APP_UNINSTALL_KEY
    !error "APP_UNINSTALL_KEY is required"
!endif
!ifndef APP_COPYRIGHT
    !error "APP_COPYRIGHT is required"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "x64.nsh"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_KEY}"
!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 100

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var InstallTransactionActive

Name "${APP_DISPLAY_NAME}"
OutFile "${OUTPUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
RequestExecutionLevel user
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
VIAddVersionKey "OriginalFilename" "${APP_NAME}_${RELEASE_TAG}_windows_amd64_setup.exe"

!define MUI_ABORTWARNING
!define MUI_CUSTOMFUNCTION_ABORT PreventInstallTransactionAbort
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
!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${VERSION} is installed"
!define MUI_FINISHPAGE_TEXT "Setup completed successfully.$\r$\n$\r$\n${APP_DISPLAY_NAME} is a command-line tool, so no application window opens.$\r$\n$\r$\nOpen a new terminal and go to a project directory.$\r$\n$\r$\nFor a new project, run:$\r$\n${APP_NAME} init$\r$\n$\r$\nFor an existing profile, run:$\r$\n${APP_NAME} up$\r$\n$\r$\nTo edit settings, run:$\r$\n${APP_NAME} config"
!define MUI_FINISHPAGE_LINK "Open setup and usage guide"
!define MUI_FINISHPAGE_LINK_LOCATION "${APP_PRODUCT_URL}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"
!insertmacro MUI_PAGE_INSTFILES
!define MUI_PAGE_CUSTOMFUNCTION_SHOW PositionInstallerFinishLink
!insertmacro MUI_PAGE_FINISH
UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function SelectInstallerWelcomeBitmap
    System::Call 'USER32::GetDpiForWindow(p $HWNDPARENT)i.r0'
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

Function PositionInstallerFinishLink
    System::Store "S"
    ${NSD_GetText} $mui.FinishPage.Text $0
    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Text, @r1)'
    System::Call '*$1(i.r2, i.r3, i.r4, i.r5)'
    IntOp $4 $4 - $2
    System::Call '*$1(i 0, i 0, i r4, i 0)'
    System::Call 'USER32::GetDC(p $mui.FinishPage.Text) p.r6'
    SendMessage $mui.FinishPage.Text ${WM_GETFONT} 0 0 $7
    System::Call 'GDI32::SelectObject(p r6, p r7) p.s'
    System::Call 'USER32::DrawTextW(p r6, w r0, i -1, p r1, i 0x00000C10)'
    System::Call '*$1(i, i, i, i.r8)'
    System::Call 'GDI32::SelectObject(p r6, p s)'
    System::Call 'USER32::ReleaseDC(p $mui.FinishPage.Text, p r6)'

    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Text, @r1)'
    System::Call 'USER32::MapWindowPoints(p 0, p $mui.FinishPage, p r1, i 2)'
    System::Call '*$1(i.r2, i.r3, i.r4, i.r5)'
    IntOp $7 $4 - $2
    System::Call 'USER32::SetWindowPos(p $mui.FinishPage.Text, p 0, i r2, i r3, i r7, i r8, i 0x14)'

    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Link, @r1)'
    System::Call 'USER32::MapWindowPoints(p 0, p $mui.FinishPage, p r1, i 2)'
    System::Call '*$1(i.r2, i.r4, i.r5, i.r6)'
    IntOp $7 $5 - $2
    IntOp $9 $6 - $4
    IntOp $8 $8 + $3
    IntOp $8 $8 + $9
    System::Call 'USER32::SetWindowPos(p $mui.FinishPage.Link, p 0, i r2, i r8, i r7, i r9, i 0x14)'
    System::Store "L"
FunctionEnd

Function .onInit
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        Abort
    ${EndIf}
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
    StrCpy $InstallTransactionActive "0"
FunctionEnd

Function PreventInstallTransactionAbort
    ${If} $InstallTransactionActive == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is completing or rolling back an installation transaction. Wait for setup to finish." /SD IDOK
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

Function un.onInit
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        Abort
    ${EndIf}
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
    StrCpy $DeleteConfigurationOnUninstall "0"
    ${GetParameters} $0
    ClearErrors
    ${GetOptions} $0 "/DELETE_CONFIG" $1
    IfErrors done
    StrCpy $DeleteConfigurationOnUninstall "1"
    done:
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

!macro UpdateUserPath ACTION
    DetailPrint "Updating the current-user PATH..."
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "/oname=path.ps1" "${PATH_HELPER}"
    ClearErrors
    nsExec::ExecToStack /TIMEOUT=15000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$PLUGINSDIR\path.ps1" -Action ${ACTION} -InstallDirectory "$INSTDIR"'
    Pop $0
    Pop $1
    ${If} $0 == "0"
    ${ElseIf} $0 == "10"
    ${Else}
        DetailPrint "Could not update the current-user PATH: $1"
        SetErrors
    ${EndIf}
    ${If} $0 == "10"
        DetailPrint "Notifying Windows about the PATH change..."
        ; WM_SETTINGCHANGE requires this synchronous API because Environment is a
        ; pointer. Keep its per-window timeout short so a hung window cannot hold setup.
        System::Call 'User32::SendMessageTimeoutW(p ${HWND_BROADCAST}, i ${WM_SETTINGCHANGE}, p 0, w "Environment", i 0x2, i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}, *p .r3)'
    ${EndIf}
!macroend

!macro BackupRuntimeFile NAME FLAG
    StrCpy ${FLAG} "0"
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        CopyFiles /SILENT "$INSTDIR\${NAME}" "$PLUGINSDIR\backup"
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Could not back up the installed ${NAME}. Close running commands and try again." /SD IDOK
            Abort
        ${EndIf}
        StrCpy ${FLAG} "1"
    ${EndIf}
!macroend

!macro ReplaceRuntimeFile NAME
    ${If} $9 == "0"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\package\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $9 "1"
        ${EndIf}
    ${EndIf}
!macroend

!macro RestoreRuntimeFile NAME FLAG
    ${If} ${FLAG} == "1"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\backup\${NAME}" "$INSTDIR"
    ${Else}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
    ${EndIf}
    ${If} ${Errors}
        StrCpy $R0 "1"
    ${EndIf}
!macroend

Section "Install"
    StrCpy $R5 "0"
    StrCpy $R6 "0"
    StrCpy $R7 "0"
    StrCpy $R9 "0"
    StrCpy $R3 ""
    StrCpy $4 "0"
    ClearErrors
    ReadRegStr $3 HKCU "${UNINSTALL_KEY}" "DisplayName"
    ${If} ${Errors}
        ClearErrors
        StrCpy $2 "0"
    ${ElseIf} $3 != "${APP_DISPLAY_NAME}"
        MessageBox MB_ICONSTOP|MB_OK "The existing installer registration is not owned by ${APP_DISPLAY_NAME}. No files were changed." /SD IDOK
        Abort
    ${Else}
        StrCpy $R5 "1"
        ClearErrors
        ReadRegStr $R3 HKCU "${UNINSTALL_KEY}" "DisplayVersion"
        ${IfNot} ${Errors}
            StrCpy $R6 "1"
        ${EndIf}
        ClearErrors
        ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"
        ${If} ${Errors}
            StrCpy $2 "0"
        ${Else}
            StrCpy $R7 "1"
        ${EndIf}
    ${EndIf}

    InitPluginsDir
    SetOutPath "$PLUGINSDIR\package"
    ClearErrors
    File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"
    File "${PACKAGE_DIR}\${APP_EXECUTABLE}"
    File "${PACKAGE_DIR}\${APP_LICENSE}"
    File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the ${APP_DISPLAY_NAME} application package." /SD IDOK
        Abort
    ${EndIf}
    CreateDirectory "$PLUGINSDIR\backup"
    CreateDirectory "$INSTDIR"
    !insertmacro BackupRuntimeFile "${APP_BASE_SCRIPT}" $6
    !insertmacro BackupRuntimeFile "${APP_EXECUTABLE}" $7
    !insertmacro BackupRuntimeFile "${APP_LICENSE}" $R1
    !insertmacro BackupRuntimeFile "${APP_STACK_SCRIPT}" $8
    !insertmacro BackupRuntimeFile "uninstall.exe" $R2

    StrCpy $InstallTransactionActive "1"
    Call DisableInstallCancellation
    StrCpy $9 "0"
    !insertmacro ReplaceRuntimeFile "${APP_EXECUTABLE}"
    !insertmacro ReplaceRuntimeFile "${APP_BASE_SCRIPT}"
    !insertmacro ReplaceRuntimeFile "${APP_LICENSE}"
    !insertmacro ReplaceRuntimeFile "${APP_STACK_SCRIPT}"
    ${If} $9 != "0"
        StrCpy $R4 "Could not replace the complete ${APP_DISPLAY_NAME} application payload."
        Goto install_rollback
    ${EndIf}

    DetailPrint "Creating the default user configuration when missing..."
    nsExec::ExecToStack /TIMEOUT=15000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-seed-configuration'
    Pop $0
    Pop $1
    ${If} $0 != "0"
        StrCpy $R4 "Could not create the ${APP_DISPLAY_NAME} user configuration: $1"
        Goto install_rollback
    ${EndIf}

    SetOutPath "$INSTDIR"
    StrCpy $R9 "1"
    ClearErrors
    WriteUninstaller "$INSTDIR\uninstall.exe"
    ${If} ${Errors}
        StrCpy $R4 "Could not create the ${APP_DISPLAY_NAME} uninstaller."
        Goto install_rollback
    ${EndIf}

    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP_DISPLAY_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${APP_PUBLISHER}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\${APP_EXECUTABLE}",0'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${APP_PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    ${If} ${Errors}
        StrCpy $R4 "Could not register ${APP_DISPLAY_NAME} in Windows Installed Apps."
        Goto install_rollback
    ${EndIf}

    !insertmacro UpdateUserPath "Add"
    StrCpy $4 $0
    ${If} $4 != "0"
    ${AndIf} $4 != "10"
        StrCpy $R4 "Could not update the current-user PATH: $1"
        Goto install_rollback
    ${EndIf}
    ${If} $2 == "1"
        StrCpy $5 "1"
    ${ElseIf} $4 == "10"
        StrCpy $5 "1"
    ${Else}
        StrCpy $5 "0"
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $5
    ${If} ${Errors}
        StrCpy $R4 "Could not record ${APP_DISPLAY_NAME} PATH ownership."
        Goto install_rollback
    ${EndIf}
    StrCpy $InstallTransactionActive "0"
    Call EnableInstallCancellation
    Goto install_done

    install_rollback:
    SetOutPath "$PLUGINSDIR"
    StrCpy $R0 "0"
    ${If} $4 == "10"
        !insertmacro UpdateUserPath "Remove"
        ${If} $0 != "0"
        ${AndIf} $0 != "10"
            StrCpy $R0 "1"
        ${EndIf}
    ${EndIf}
    ${If} $R9 == "1"
        ${If} $R5 == "1"
            ${If} $R6 == "1"
                ClearErrors
                WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" $R3
                ${If} ${Errors}
                    StrCpy $R0 "1"
                ${EndIf}
            ${Else}
                ClearErrors
                ReadRegStr $R8 HKCU "${UNINSTALL_KEY}" "DisplayVersion"
                ${IfNot} ${Errors}
                    ClearErrors
                    DeleteRegValue HKCU "${UNINSTALL_KEY}" "DisplayVersion"
                    ${If} ${Errors}
                        StrCpy $R0 "1"
                    ${EndIf}
                ${EndIf}
            ${EndIf}
            ${If} $R7 == "1"
                ClearErrors
                WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $2
                ${If} ${Errors}
                    StrCpy $R0 "1"
                ${EndIf}
            ${Else}
                ClearErrors
                ReadRegDWORD $R8 HKCU "${UNINSTALL_KEY}" "PathAdded"
                ${IfNot} ${Errors}
                    ClearErrors
                    DeleteRegValue HKCU "${UNINSTALL_KEY}" "PathAdded"
                    ${If} ${Errors}
                        StrCpy $R0 "1"
                    ${EndIf}
                ${EndIf}
            ${EndIf}
        ${Else}
            ClearErrors
            EnumRegValue $R8 HKCU "${UNINSTALL_KEY}" 0
            ${IfNot} ${Errors}
                ClearErrors
                DeleteRegKey HKCU "${UNINSTALL_KEY}"
                ${If} ${Errors}
                    StrCpy $R0 "1"
                ${EndIf}
            ${EndIf}
        ${EndIf}
    ${EndIf}
    !insertmacro RestoreRuntimeFile "${APP_EXECUTABLE}" $7
    !insertmacro RestoreRuntimeFile "${APP_BASE_SCRIPT}" $6
    !insertmacro RestoreRuntimeFile "${APP_LICENSE}" $R1
    !insertmacro RestoreRuntimeFile "${APP_STACK_SCRIPT}" $8
    !insertmacro RestoreRuntimeFile "uninstall.exe" $R2
    RMDir "$INSTDIR"
    StrCpy $InstallTransactionActive "0"
    Call EnableInstallCancellation
    ${If} $R0 == "0"
        MessageBox MB_ICONSTOP|MB_OK "$R4 The prior installer-owned state was restored. Close running commands and try again." /SD IDOK
    ${Else}
        MessageBox MB_ICONSTOP|MB_OK "$R4 Rollback was incomplete. Close running commands and run setup again." /SD IDOK
    ${EndIf}
    Abort

    install_done:
SectionEnd

Section "Uninstall"
    SetAutoClose true
    SetOutPath "$TEMP"
    ClearErrors
    ReadRegStr $3 HKCU "${UNINSTALL_KEY}" "DisplayName"
    ${If} ${Errors}
        ClearErrors
        StrCpy $2 "0"
        StrCpy $4 "0"
    ${ElseIf} $3 != "${APP_DISPLAY_NAME}"
        MessageBox MB_ICONSTOP|MB_OK "The installer registration is not owned by ${APP_DISPLAY_NAME}. No files were changed." /SD IDOK
        Abort
    ${Else}
        StrCpy $4 "1"
        ClearErrors
        ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"
        ${If} ${Errors}
            StrCpy $2 "0"
        ${EndIf}
    ${EndIf}

    DetailPrint "Removing ${APP_DISPLAY_NAME} state, SSH integration, and cache; any running Sandbox stays open..."
    ${If} $DeleteConfigurationOnUninstall == "1"
        nsExec::ExecToStack /TIMEOUT=960000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --delete-configuration'
    ${Else}
        nsExec::ExecToStack /TIMEOUT=960000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall'
    ${EndIf}
    Pop $0
    Pop $1
    ${If} $0 != "0"
        MessageBox MB_ICONSTOP|MB_OK "Could not safely remove ${APP_DISPLAY_NAME} state. No application files or installer registration were removed: $1" /SD IDOK
        Abort
    ${EndIf}

    ClearErrors
    Delete "$INSTDIR\${APP_EXECUTABLE}"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Close any running ${APP_DISPLAY_NAME} command and try uninstalling again. No running process was terminated." /SD IDOK
        Abort
    ${EndIf}
    ClearErrors
    Delete "$INSTDIR\${APP_BASE_SCRIPT}"
    Delete "$INSTDIR\${APP_LICENSE}"
    Delete "$INSTDIR\${APP_STACK_SCRIPT}"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not remove the installed ${APP_DISPLAY_NAME} files. Check their permissions and try again." /SD IDOK
        Abort
    ${EndIf}
    ${If} $2 == "1"
        !insertmacro UpdateUserPath "Remove"
        ${If} $0 != "0"
        ${AndIf} $0 != "10"
            MessageBox MB_ICONSTOP|MB_OK "Could not remove the installer-owned ${APP_DISPLAY_NAME} PATH entry: $1" /SD IDOK
            Abort
        ${EndIf}
    ${EndIf}
    ${If} $4 == "1"
        ClearErrors
        DeleteRegKey HKCU "${UNINSTALL_KEY}"
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Could not remove ${APP_DISPLAY_NAME} from Windows Installed Apps." /SD IDOK
            Abort
        ${EndIf}
    ${EndIf}
    ClearErrors
    Delete "$INSTDIR\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not remove the ${APP_DISPLAY_NAME} uninstaller." /SD IDOK
        Abort
    ${EndIf}
    ClearErrors
    RMDir "$INSTDIR"
SectionEnd
