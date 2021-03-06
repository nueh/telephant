import QtQuick 2.4
import QtQuick.Controls 2.1
import QtQuick.Controls.Material 2.1
import QtQuick.Layouts 1.3

Popup {
    property var message

    id: popup

    modal: true
    focus: true
    height: Math.min(mainWindow.height * 0.8, layout.implicitHeight + 32)
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutsideParent

    Flickable {
        id: flickable
        anchors.fill: parent
        clip: true
        contentHeight: layout.height

        ColumnLayout {
            id: layout
            width: parent.width

            MessageView {
                showActionButtons: false
                visible: message != null
                message: popup.message
            }

            Label {
                visible: message != null
                text: qsTr("Replying to %1").arg(message.name)
                opacity: 0.3
            }

            TextArea {
                id: messageArea
                Layout.fillWidth: true
                Layout.minimumHeight: 128
                focus: true
                selectByMouse: true
                placeholderText: message != null ? qsTr("Post your reply") : qsTr("What's happening?")
                wrapMode: TextArea.Wrap
            }

            RowLayout {
                Layout.alignment: Qt.AlignRight

                Label {
                    id: remCharsLabel

                    Layout.alignment: Qt.AlignVCenter | Qt.AlignRight

                    font.pointSize: 12
                    text: accountBridge.postSizeLimit - uiBridge.postLimitCount(messageArea.text)
                }

                Button {
                    id: sendButton
                    enabled: remCharsLabel.text >= 0 && messageArea.text.length > 0
                    Layout.alignment: Qt.AlignBottom | Qt.AlignRight
                    highlighted: true
                    text: message != null ? qsTr("Reply") : qsTr("Post")

                    onClicked: {
                        popup.close()
                        var msg = messageArea.text
                        var msgid = "";
                        if (message != null) {
                            msgid = message.messageid
                            msg = "@" + message.author + " " + msg
                        }
                        uiBridge.postButton(msgid, msg)
                        messageArea.clear()
                    }
                }
            }
        }
    }
}
